package central

import (
	"encoding/json"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"io"
	"math"
	"os"
	"strings"
	"time"
)

const pgTrafficPath = "/var/lib/pasarguard/marzwatch-traffic.json"

type pgUsage struct {
	RX       uint64  `json:"rx"`
	TX       uint64  `json:"tx"`
	RXBps    float64 `json:"rx_bps"`
	TXBps    float64 `json:"tx_bps"`
	Last     float64 `json:"last"`
	Complete bool    `json:"complete"`
}
type pgSnapshot struct {
	Schema    int                `json:"schema"`
	Source    string             `json:"source"`
	Window    int                `json:"window_seconds"`
	Generated float64            `json:"generated"`
	Nodes     map[string]pgUsage `json:"nodes"`
}

func readPGTraffic(path string, now time.Time) (pgSnapshot, bool) {
	var v pgSnapshot
	f, err := os.Open(path)
	if err != nil {
		return v, false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1048577))
	if err != nil || len(b) > 1048576 || json.Unmarshal(b, &v) != nil {
		return v, false
	}
	t := float64(now.UnixNano()) / 1e9
	if v.Schema != 1 || v.Source != "xray-inbound-no-reset" || v.Window != 86400 || t-v.Generated > 35 || v.Generated > t+5 {
		return v, false
	}
	return v, true
}

func (v pgSnapshot) usage(id string, now time.Time) (pgUsage, bool) {
	u, ok := v.Nodes[id]
	t := float64(now.UnixNano()) / 1e9
	return u, ok && id != "" && t-u.Last <= 35 && u.Last <= t+5 && u.Last > 0 &&
		u.RXBps >= 0 && u.TXBps >= 0 && !math.IsNaN(u.RXBps) && !math.IsNaN(u.TXBps) &&
		!math.IsInf(u.RXBps, 0) && !math.IsInf(u.TXBps, 0) && u.RX <= math.MaxUint64-u.TX
}

func replacePGTraffic(text, value string, networkOK bool) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if pos := strings.Index(line, "📦 TODAY"); pos >= 0 {
			lines[i] = line[:pos] + "📦 24H  " + value
		}
		if !networkOK && (strings.Contains(lines[i], "⬇️") || strings.Contains(lines[i], "↕️")) {
			if p := strings.Index(lines[i], "⬇️"); p >= 0 {
				lines[i] = lines[i][:p] + "⬇️ N/A   ⬆️ N/A"
			}
			if p := strings.Index(lines[i], "↕️"); p >= 0 {
				lines[i] = lines[i][:p] + "↕️ N/A"
			}
		}
	}
	return strings.Join(lines, "\n")
}

func (s *Server) formatPGNode(n model.Node, on bool, date string) string {
	if !s.cfg.PasarGuardTraffic {
		return formatNodeBlock(n, on, date)
	}
	now := time.Now()
	v, valid := readPGTraffic(pgTrafficPath, now)
	u, ok := v.usage(s.cfg.PasarGuardNodes[n.ID], now)
	ok = ok && valid
	value := "N/A"
	n.Latest.RXBps, n.Latest.TXBps = 0, 0
	if ok {
		value = bytes(u.RX + u.TX)
		if !u.Complete {
			value += " *"
		}
		n.Latest.RXBps, n.Latest.TXBps = pgBitsPerSecond(u.RXBps), pgBitsPerSecond(u.TXBps)
	}
	return replacePGTraffic(formatNodeBlock(n, on, date), value, ok)
}

func (s *Server) formatPGOverview(nodes []model.Node, now time.Time) string {
	if !s.cfg.PasarGuardTraffic {
		return formatOverviewLive(nodes, now)
	}
	v, valid := readPGTraffic(pgTrafficPath, now)
	value, rx, tx, ok := summarizePG(v, now)
	ok = ok && valid
	if !ok {
		value = "N/A"
	}
	// Sum by panel identity, not monitor cards: aliases cannot double count,
	// and unrelated relay/host-only cards do not enter the proxy total.
	out := replacePGTraffic(formatOverviewLive(nodes, now), value, ok)
	if !ok {
		return out
	}
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if p := strings.Index(line, "⬇️"); p >= 0 {
			lines[i] = line[:p] + "⬇️ " + rate(rx) + "   ⬆️ " + rate(tx)
		}
		if p := strings.Index(line, "↕️"); p >= 0 {
			lines[i] = line[:p] + "↕️ " + rate(rx+tx)
		}
	}
	return strings.Join(lines, "\n")
}

func summarizePG(v pgSnapshot, now time.Time) (string, float64, float64, bool) {
	var total uint64
	var rx, tx float64
	complete, any := true, false
	for id, u := range v.Nodes {
		_, fresh := v.usage(id, now)
		if u.Last <= 0 || u.Last > float64(now.Unix())+5 || u.RX > math.MaxUint64-u.TX {
			complete = false
			continue
		}
		if u.RX+u.TX > math.MaxUint64-total {
			return "N/A", 0, 0, false
		}
		total += u.RX + u.TX
		any = true
		complete = complete && u.Complete && fresh
		if fresh {
			rx += pgBitsPerSecond(u.RXBps)
			tx += pgBitsPerSecond(u.TXBps)
		}
	}
	value := bytes(total)
	if !complete {
		value += " *"
	}
	return value, rx, tx, any
}

// The independent meter stores bytes/second; model rates and rate() use bits/second.
func pgBitsPerSecond(bytesPerSecond float64) float64 { return bytesPerSecond * 8 }
