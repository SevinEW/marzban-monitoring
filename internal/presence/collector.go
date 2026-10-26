package presence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protowire"
)

const ConfigPath = "/etc/marzwatch/presence.json"

type Core struct {
	Address    string `json:"address"`
	PID        int    `json:"pid"`
	StartTicks string `json:"start_ticks"`
}
type Binding struct {
	Schema           int    `json:"schema"`
	Namespace        string `json:"namespace"`
	TrackingVerified bool   `json:"tracking_verified"`
	Cores            []Core `json:"cores"`
}
type Sample struct {
	Status     string
	Users      []string
	Generation string
}
type rawMessage []byte
type rawCodec struct{}

func (rawCodec) Name() string { return "proto" }
func (rawCodec) Marshal(v any) ([]byte, error) {
	p, ok := v.(*rawMessage)
	if !ok {
		return nil, errors.New("invalid RPC type")
	}
	return []byte(*p), nil
}
func (rawCodec) Unmarshal(b []byte, v any) error {
	p, ok := v.(*rawMessage)
	if !ok {
		return errors.New("invalid RPC type")
	}
	*p = append((*p)[:0], b...)
	return nil
}

func parseUsers(b []byte, namespace string) ([]string, error) {
	users := map[string]struct{}{}
	for len(b) > 0 {
		n, t, k := protowire.ConsumeTag(b)
		if k < 0 {
			return nil, errors.New("invalid protobuf")
		}
		b = b[k:]
		if n != 1 || t != protowire.BytesType {
			skip := protowire.ConsumeFieldValue(n, t, b)
			if skip < 0 {
				return nil, errors.New("invalid field")
			}
			b = b[skip:]
			continue
		}
		name, size := protowire.ConsumeString(b)
		if size < 0 || len(name) > 1024 {
			return nil, errors.New("invalid identity")
		}
		b = b[size:]
		if !strings.HasPrefix(name, "user>>>") || !strings.HasSuffix(name, ">>>online") {
			return nil, errors.New("unsupported identity")
		}
		user := strings.TrimSuffix(strings.TrimPrefix(name, "user>>>"), ">>>online")
		if user == "" || strings.Contains(user, ">>>") || strings.ContainsAny(user, "\r\n\x00") {
			return nil, errors.New("invalid identity")
		}
		h := sha256.Sum256([]byte(namespace + "\x00" + user))
		users[hex.EncodeToString(h[:16])] = struct{}{}
		if len(users) > MaxUsers {
			return nil, errors.New("too many identities")
		}
	}
	out := make([]string, 0, len(users))
	for u := range users {
		out = append(out, u)
	}
	sort.Strings(out)
	return out, nil
}
func ProcessStart(pid int) (string, error) {
	if pid <= 1 {
		return "", errors.New("invalid PID")
	}
	b, e := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if e != nil {
		return "", e
	}
	pos := strings.LastIndexByte(string(b), ')')
	if pos < 0 {
		return "", errors.New("invalid process")
	}
	fields := strings.Fields(string(b[pos+1:]))
	if len(fields) <= 19 || fields[0] == "Z" {
		return "", errors.New("process ended")
	}
	return fields[19], nil
}
func ValidateBinding(c Binding) error {
	if c.Schema != 1 || len(c.Namespace) < 16 || len(c.Namespace) > 128 || !c.TrackingVerified || len(c.Cores) == 0 || len(c.Cores) > 4 {
		return errors.New("unverified source")
	}
	seen := map[string]bool{}
	for _, core := range c.Cores {
		host, port, e := net.SplitHostPort(core.Address)
		ip := net.ParseIP(host)
		p, _ := strconv.Atoi(port)
		if e != nil || ip == nil || !ip.IsLoopback() || p < 1 || p > 65535 || core.PID <= 1 || core.StartTicks == "" || seen[core.Address] {
			return errors.New("invalid source")
		}
		seen[core.Address] = true
	}
	return nil
}

type Collector struct {
	path       string
	binding    Binding
	generation string
	checked    time.Time
	conns      map[string]*grpc.ClientConn
	state      string
}

func NewCollector(path string) *Collector {
	return &Collector{path: path, conns: map[string]*grpc.ClientConn{}, state: "unconfigured"}
}
func (c *Collector) Close() {
	for _, conn := range c.conns {
		conn.Close()
	}
	c.conns = map[string]*grpc.ClientConn{}
}
func (c *Collector) reload(now time.Time) {
	if now.Sub(c.checked) < 5*time.Second {
		return
	}
	c.checked = now
	f, e := os.Open(c.path)
	if e != nil {
		c.Close()
		c.state = "unconfigured"
		return
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 65537))
	var binding Binding
	if e != nil || len(b) > 65536 || json.Unmarshal(b, &binding) != nil || ValidateBinding(binding) != nil {
		c.Close()
		c.state = "source_changed"
		return
	}
	digest := sha256.Sum256(b)
	gen := hex.EncodeToString(digest[:16])
	if gen != c.generation {
		c.Close()
		c.binding = binding
		c.generation = gen
	}
	c.state = "ok"
}
func (c *Collector) Sample(ctx context.Context) Sample {
	c.reload(time.Now())
	result := Sample{Status: c.state, Generation: c.generation}
	if c.state != "ok" {
		return result
	}
	all := map[string]struct{}{}
	for _, core := range c.binding.Cores {
		start, e := ProcessStart(core.PID)
		if e != nil || start != core.StartTicks {
			result.Status = "source_changed"
			return result
		}
		conn := c.conns[core.Address]
		if conn == nil {
			conn, e = grpc.NewClient("passthrough:///"+core.Address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.ForceCodec(rawCodec{}), grpc.MaxCallRecvMsgSize(MaxBody)))
			if e != nil {
				result.Status = "unavailable"
				return result
			}
			c.conns[core.Address] = conn
		}
		var request, response rawMessage
		call, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
		e = conn.Invoke(call, "/xray.app.stats.command.StatsService/GetAllOnlineUsers", &request, &response)
		cancel()
		if e != nil {
			result.Status = "unavailable"
			return result
		}
		users, e := parseUsers(response, c.binding.Namespace)
		if e != nil {
			result.Status = "unsupported"
			return result
		}
		// A restart during the call must not publish data against an old attestation.
		after, e := ProcessStart(core.PID)
		if e != nil || after != start {
			result.Status = "source_changed"
			return result
		}
		for _, u := range users {
			all[u] = struct{}{}
		}
		if len(all) > MaxUsers {
			result.Status = "unavailable"
			return result
		}
	}
	result.Users = make([]string, 0, len(all))
	for u := range all {
		result.Users = append(result.Users, u)
	}
	sort.Strings(result.Users)
	return result
}
