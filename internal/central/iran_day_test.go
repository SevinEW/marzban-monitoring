package central

import (
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/reportclock"
	"strings"
	"testing"
	"time"
)

func TestLiveTrafficRollsOverWithoutNewSample(t *testing.T) {
	mid := time.Date(2026, 10, 11, 20, 30, 0, 0, time.UTC)
	n := model.Node{Name: "node", LastSeen: mid.Add(-time.Second), Daily: map[string]model.DailyStats{"2026-10-11": {RXBytes: 1073741824}}}
	before := formatOverviewLive([]model.Node{n}, mid.Add(-time.Second).In(reportclock.Iran))
	after := formatOverviewLive([]model.Node{n}, mid.In(reportclock.Iran))
	if !strings.Contains(before, bytes(1073741824)) || strings.Contains(after, bytes(1073741824)) {
		t.Fatal("Overview retained yesterday's traffic")
	}
	card := formatNodeBlock(n, true, reportclock.Date(mid))
	if strings.Contains(card, bytes(1073741824)) {
		t.Fatal("node retained yesterday's traffic")
	}
}
