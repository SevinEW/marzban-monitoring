package collector

import (
	"bufio"
	"fmt"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Read kernel counters at the device boundary. Device-backed NICs include VM
// NICs; bridge/bond/VLAN/WARP/veth layers have no device and are not added again.
// No route, firewall, packet capture, DB query or external request is involved.
func readNetwork(path, sys string) ([]model.NetCounter, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseNetwork(f, sys)
}

func parseNetwork(input io.Reader, sys string) ([]model.NetCounter, error) {
	s := bufio.NewScanner(io.LimitReader(input, 1<<20))
	var counters []model.NetCounter
	for s.Scan() {
		parts := strings.SplitN(s.Text(), ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "lo" || name == "" || filepath.Base(name) != name {
			continue
		}
		root := filepath.Join(sys, name)
		if _, err := os.Stat(filepath.Join(root, "device")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			return nil, fmt.Errorf("invalid NIC counters")
		}
		rx, e1 := strconv.ParseUint(fields[0], 10, 64)
		tx, e2 := strconv.ParseUint(fields[8], 10, 64)
		b, e3 := os.ReadFile(filepath.Join(root, "ifindex"))
		index, e4 := strconv.Atoi(strings.TrimSpace(string(b)))
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || index < 1 {
			return nil, fmt.Errorf("invalid NIC identity/counters")
		}
		counters = append(counters, model.NetCounter{Name: name, Index: index, RX: rx, TX: tx})
		if len(counters) > 64 {
			return nil, fmt.Errorf("too many physical NICs")
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(counters) == 0 {
		return nil, fmt.Errorf("no verified physical NIC")
	}
	sort.Slice(counters, func(i, j int) bool { return counters[i].Name < counters[j].Name })
	return counters, nil
}
