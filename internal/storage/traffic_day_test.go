package storage

import (
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/reportclock"
	"path/filepath"
	"testing"
	"time"
)

func TestIranMidnightTrafficAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// 20:30 UTC is midnight Iran, regardless of the caller/server timezone.
	mid := time.Date(2026, 10, 11, 20, 30, 0, 0, time.UTC)
	s.UpsertNode(model.Node{ID: "n", Daily: map[string]model.DailyStats{}, Latest: model.Metric{Timestamp: mid.Add(-2 * time.Second), RXBytesTotal: 100, TXBytesTotal: 200}})
	n, ok := s.UpdateMetric("n", model.Metric{Timestamp: mid.Add(3 * time.Second), RXBytesTotal: 1100, TXBytesTotal: 700}, time.UTC)
	if !ok {
		t.Fatal("metric rejected")
	}
	old, current := n.Daily["2026-10-11"], n.Daily["2026-10-12"]
	if old.RXBytes != 400 || old.TXBytes != 200 || current.RXBytes != 600 || current.TXBytes != 300 {
		t.Fatalf("old=%+v new=%+v", old, current)
	}
	if err = s.Flush(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	n, _ = s.UpdateMetric("n", model.Metric{Timestamp: mid.Add(8 * time.Second), RXBytesTotal: 1600, TXBytesTotal: 950}, time.FixedZone("other", -8*3600))
	if n.Daily["2026-10-12"].RXBytes != 1100 || n.Daily["2026-10-11"].RXBytes != 400 {
		t.Fatalf("restart lost/doubled traffic: %+v", n.Daily)
	}
	// Duplicate packets and rebooted counters must not inflate either day.
	s.UpdateMetric("n", n.Latest, time.UTC)
	n, _ = s.UpdateMetric("n", model.Metric{Timestamp: mid.Add(13 * time.Second), RXBytesTotal: 10, TXBytesTotal: 20}, time.UTC)
	if n.Daily["2026-10-12"].RXBytes != 1100 {
		t.Fatal("counter reset counted as traffic")
	}
}

func TestIranMidnightExactBoundary(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "state.json"))
	mid := time.Date(2026, 7, 2, 0, 0, 0, 0, reportclock.Iran)
	s.UpsertNode(model.Node{ID: "n", Latest: model.Metric{Timestamp: mid.Add(-5 * time.Second), RXBytesTotal: 1}})
	n, _ := s.UpdateMetric("n", model.Metric{Timestamp: mid, RXBytesTotal: 502}, time.UTC)
	if n.Daily["2026-07-01"].RXBytes != 501 || n.Daily["2026-07-02"].RXBytes != 0 {
		t.Fatalf("midnight: %+v", n.Daily)
	}
	_, offset := mid.Zone()
	if offset != 12600 {
		t.Fatalf("Iran summer offset %d", offset)
	}
}

func TestTrafficSplitConservesLargeCounters(t *testing.T) {
	mid := time.Date(2026, 10, 12, 0, 0, 0, 0, reportclock.Iran)
	n := model.Node{Daily: map[string]model.DailyStats{}}
	amount := uint64(1<<63) + 17
	addDailyTraffic(&n, model.Metric{Timestamp: mid.Add(-time.Second)}, model.Metric{Timestamp: mid.Add(2 * time.Second), RXBytesTotal: amount})
	if n.Daily["2026-10-11"].RXBytes+n.Daily["2026-10-12"].RXBytes != amount {
		t.Fatal("bytes lost in split")
	}
}
