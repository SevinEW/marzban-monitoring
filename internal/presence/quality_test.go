package presence

import (
	"testing"
	"time"
)

func TestAuditedBlindSpotNeverCountsAsHealthyZeroOrTotal(t *testing.T) {
	now := time.Now()
	r := NewRegistry()
	r.Apply("good", packet(1, true, []string{ident(1)}, nil), now)
	r.Apply("blind", packet(1, true, nil, nil), now)
	blocks := map[string]string{"blind": "loopback_untracked"}
	for seq, users := range [][]string{nil, {ident(1), ident(2)}} {
		r.Apply("blind", packet(uint64(seq+2), true, users, nil), now)
		views, summary := r.SnapshotWithQuality([]string{"good", "blind", "blind"}, now, blocks)
		if views["blind"].Users != nil || views["blind"].Status != "loopback_untracked" || summary.Ready != 1 || summary.Expected != 2 || summary.Users != nil || *summary.ObservedUsers != 1 {
			t.Fatalf("blind spot leaked into healthy coverage: %+v %+v", views, summary)
		}
	}
	_, restored := r.SnapshotWithQuality([]string{"good", "blind"}, now, nil)
	if restored.Users == nil || *restored.Users != 2 || restored.Ready != 2 {
		t.Fatal("removing a resolved defect did not restore deduplicated live data")
	}
	views, none := r.SnapshotWithQuality([]string{"blind"}, now, map[string]string{"blind": "future_reason"})
	if none.Users != nil || none.ObservedUsers != nil || none.Expected != 1 || views["blind"].Status != "coverage_unverified" {
		t.Fatal("unknown quality defect failed open")
	}
}
