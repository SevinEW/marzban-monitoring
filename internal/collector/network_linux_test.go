package collector

import (
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPhysicalBoundaryExcludesStackedInterfaces(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"eth0", "eth1", "wgcf", "docker0", "veth1", "bond0", "eth0.10"} {
		root := filepath.Join(dir, name)
		os.MkdirAll(root, 0700)
		os.WriteFile(filepath.Join(root, "ifindex"), []byte("2"), 0600)
		if name == "eth0" || name == "eth1" {
			os.Mkdir(filepath.Join(root, "device"), 0700)
		}
	}
	rows := "lo: 9999 0 0 0 0 0 0 0 9999 0 0 0 0 0 0 0\n"
	for _, name := range []string{"eth0", "eth1", "wgcf", "docker0", "veth1", "bond0", "eth0.10"} {
		rows += name + ": 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n"
	}
	got, e := parseNetwork(strings.NewReader(rows), dir)
	if e != nil || len(got) != 2 || got[0].Name != "eth0" || got[1].Name != "eth1" {
		t.Fatal(got, e)
	}
	// IP aliases never appear as extra kernel devices/counters.
	if _, e = parseNetwork(strings.NewReader("wgcf: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n"), dir); e == nil {
		t.Fatal("unverified fallback")
	}
}

func TestEarlyWakePreservesBytesWithoutBurstRate(t *testing.T) {
	c := New()
	base := time.Unix(1000, 0)
	counters := func(rx uint64) []model.NetCounter {
		return []model.NetCounter{{Name: "eth0", Index: 2, RX: rx, TX: rx / 2}}
	}
	prime := model.Metric{}
	c.networkSample(&prime, counters(1000), base)
	early := model.Metric{}
	c.networkSample(&early, counters(2000), base.Add(5*time.Millisecond))
	if early.NetworkReady || early.RXBps != 0 {
		t.Fatal("millisecond rate published", early)
	}
	next := model.Metric{}
	c.networkSample(&next, counters(101000), base.Add(time.Second))
	if !next.NetworkReady || next.NetworkSampleSeconds != 1 || next.RXBps != 800000 || next.TXBps != 400000 {
		t.Fatal("bytes lost or time compressed", next)
	}
	gap := model.Metric{}
	c.networkSample(&gap, counters(201000), base.Add(20*time.Second))
	if gap.NetworkReady {
		t.Fatal("gap published as live")
	}
	reset := model.Metric{}
	c.networkSample(&reset, counters(1), base.Add(21*time.Second))
	if reset.NetworkReady {
		t.Fatal("reset published as live")
	}
}

func TestNICDeltasAndDiskReservation(t *testing.T) {
	a := []model.NetCounter{{Name: "eth0", Index: 2, RX: 100, TX: 200}, {Name: "eth1", Index: 3, RX: 100, TX: 200}}
	b := append([]model.NetCounter(nil), a...)
	b[0].RX += 1000
	b[0].TX += 500
	rx, tx, ok := model.NetworkDelta(a, b)
	if !ok || rx != 1000 || tx != 500 {
		t.Fatal(rx, tx, ok)
	}
	b[1].RX = 1
	if _, _, ok = model.NetworkDelta(a, b); ok {
		t.Fatal("individual reset masked by aggregate growth")
	}
	b = append([]model.NetCounter(nil), a...)
	b[0].Index = 8
	if _, _, ok = model.NetworkDelta(a, b); ok {
		t.Fatal("NIC recreation")
	}
	used, total, avail, reserved := diskUsage(syscall.Statfs_t{Blocks: 1000, Bfree: 400, Bavail: 300, Bsize: 4096})
	if used != 600*4096 || total != 1000*4096 || avail != 300*4096 || reserved != 100*4096 {
		t.Fatal(used, total, avail, reserved)
	}
}
