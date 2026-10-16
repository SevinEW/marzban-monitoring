package central

import (
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func hostTestNode(id string, stamp int64, rx uint64) model.Node {
	t := time.Unix(stamp, 0)
	return model.Node{ID: id, LastContact: t, LastSeen: t, Latest: model.Metric{Timestamp: t, RXBytesTotal: rx, TXBytesTotal: rx / 2, UptimeSeconds: uint64(stamp), RXBps: 1000000000, TXBps: 1000000000}}
}
func TestHostRollingWindowAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.json")
	h, err := openHostTraffic(path)
	if err != nil {
		t.Fatal(err)
	}
	h.sample([]model.Node{hostTestNode("iran", 100000, 1000)}, time.Unix(100000, 0))
	h.sample([]model.Node{hostTestNode("iran", 100010, 2000)}, time.Unix(100010, 0))
	if got := h.value("iran", time.Unix(100010, 0)); got != bytes(1500)+" *" {
		t.Fatal(got)
	}
	if err := h.save(); err != nil {
		t.Fatal(err)
	}
	h, err = openHostTraffic(path)
	if err != nil {
		t.Fatal(err)
	}
	h.sample([]model.Node{hostTestNode("iran", 100010, 2000)}, time.Unix(100015, 0))
	if got := h.value("iran", time.Unix(100015, 0)); got != bytes(1500)+" *" {
		t.Fatal(got)
	}
	total, _ := hostSum(h.data.Nodes["iran"], 186405)
	if total != 750 {
		t.Fatal(total)
	}
	total, _ = hostSum(h.data.Nodes["iran"], 186410)
	if total != 0 {
		t.Fatal(total)
	}
}
func TestHostResetNewNodeAndBoundedBuckets(t *testing.T) {
	h, _ := openHostTraffic(filepath.Join(t.TempDir(), "host.json"))
	for i := int64(0); i <= 90000; i += 5 {
		stamp := 100000 + i
		h.sample([]model.Node{hostTestNode("one", stamp, uint64(i)*100)}, time.Unix(stamp, 0))
	}
	if len(h.data.Nodes["one"].Buckets) > 1443 {
		t.Fatal(len(h.data.Nodes["one"].Buckets))
	}
	total, complete := hostSum(h.data.Nodes["one"], 190000)
	if total != 86400*150 || !complete {
		t.Fatal(total, complete)
	}
	h.sample([]model.Node{hostTestNode("one", 190005, 10), hostTestNode("new", 190005, 999999999)}, time.Unix(190005, 0))
	if h.value("new", time.Unix(190005, 0)) != bytes(0)+" *" {
		t.Fatal("new lifetime imported")
	}
	if !strings.HasSuffix(h.value("one", time.Unix(190005, 0)), " *") {
		t.Fatal("reset not marked")
	}
}
func TestHostUsesExistingBitRatesWithoutPanelMapping(t *testing.T) {
	h, _ := openHostTraffic(filepath.Join(t.TempDir(), "host.json"))
	now := time.Now()
	n := hostTestNode("iran", now.Unix(), 10)
	h.sample([]model.Node{n}, now)
	s := &Server{host: h}
	out := s.formatHostNode(n, true, "2026-10-16")
	if strings.Contains(out, "N/A") || !strings.Contains(out, "2.00 Gbps") || !strings.Contains(out, "24H") {
		t.Fatal(out)
	}
}
