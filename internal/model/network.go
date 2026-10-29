package model

// NetworkSource identifies the measurement boundary, not an IP address. Aliases
// and Floating IP changes on a NIC must not reset its counters.
const NetworkSource = "physical-nic-v2"

type NetCounter struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
	RX    uint64 `json:"rx"`
	TX    uint64 `json:"tx"`
}

// NetworkDelta rejects changed topology and individual counter resets before
// summing: an increasing counter on one NIC cannot hide a reset on another.
func NetworkDelta(previous, current []NetCounter) (rx, tx uint64, valid bool) {
	if len(previous) == 0 || len(previous) != len(current) || len(current) > 64 {
		return
	}
	seen := map[string]bool{}
	for i, n := range current {
		p := previous[i]
		if n.Name == "" || n.Index < 1 || seen[n.Name] || p.Name != n.Name || p.Index != n.Index || n.RX < p.RX || n.TX < p.TX {
			return 0, 0, false
		}
		seen[n.Name] = true
		rx += n.RX - p.RX
		tx += n.TX - p.TX
	}
	return rx, tx, true
}
