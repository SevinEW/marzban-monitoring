package presence

import (
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// VerifySocketOwner is a one-time root operation, never part of the poll loop.
// Pin the actual existing API socket to the attested Xray process.
func VerifySocketOwner(core Core) error {
	root := "/proc/" + strconv.Itoa(core.PID)
	exe, e := os.Readlink(root + "/exe")
	if e != nil || !strings.HasPrefix(filepath.Base(exe), "xray") {
		return errors.New("not an Xray process")
	}
	wantHost, wantPort, e := net.SplitHostPort(core.Address)
	if e != nil {
		return e
	}
	wantIP := net.ParseIP(wantHost)
	port, _ := strconv.Atoi(wantPort)
	sockets := map[string]bool{}
	fds, e := os.ReadDir(root + "/fd")
	if e != nil {
		return errors.New("cannot verify socket ownership")
	}
	for _, fd := range fds {
		link, e := os.Readlink(root + "/fd/" + fd.Name())
		if e == nil && strings.HasPrefix(link, "socket:[") {
			sockets[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] = true
		}
	}
	for _, family := range []string{"tcp", "tcp6"} {
		raw, e := os.ReadFile(root + "/net/" + family)
		if e != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[3] != "0A" || !sockets[fields[9]] {
				continue
			}
			parts := strings.Split(fields[1], ":")
			if len(parts) != 2 {
				continue
			}
			p, e := strconv.ParseUint(parts[1], 16, 16)
			if e != nil || int(p) != port {
				continue
			}
			address, e := hex.DecodeString(parts[0])
			if e != nil || (len(address) != 4 && len(address) != 16) {
				continue
			}
			for i := 0; i < len(address); i += 4 {
				address[i], address[i+3] = address[i+3], address[i]
				address[i+1], address[i+2] = address[i+2], address[i+1]
			}
			if net.IP(address).Equal(wantIP) {
				return nil
			}
		}
	}
	return errors.New("API socket does not belong to the attested core")
}
