package central

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPGSnapshotRejectsStaleAndInvalidSources(t *testing.T) {
	now := time.Unix(200000, 0)
	path := filepath.Join(t.TempDir(), "traffic.json")
	v := pgSnapshot{Schema: 1, Source: "xray-inbound-no-reset", Window: 86400, Generated: 200000}
	for _, tc := range []struct {
		age    float64
		source string
		ok     bool
	}{
		{0, "xray-inbound-no-reset", true}, {36, "xray-inbound-no-reset", false},
		{-6, "xray-inbound-no-reset", false}, {0, "host-interface", false},
	} {
		v.Generated = 200000 - tc.age
		v.Source = tc.source
		b, _ := json.Marshal(v)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		_, ok := readPGTraffic(path, now)
		if ok != tc.ok {
			t.Fatalf("case %+v got %v", tc, ok)
		}
	}
}

func TestPGSummaryRetainsOfflineHistoryAndMarksIncomplete(t *testing.T) {
	now := time.Unix(200000, 0)
	v := pgSnapshot{Nodes: map[string]pgUsage{
		"1": {RX: 100, TX: 200, RXBps: 10, TXBps: 20, Last: 200000, Complete: true},
		"2": {RX: 400, TX: 500, RXBps: 1000, TXBps: 2000, Last: 199000, Complete: true},
	}}
	value, rx, tx, ok := summarizePG(v, now)
	if !ok || value != bytes(1200)+" *" || rx != 10 || tx != 20 {
		t.Fatalf("%q %v %v %v", value, rx, tx, ok)
	}
}

func TestPGDisplayDoesNotLeakHostTotalsOrRatesOnFailure(t *testing.T) {
	text := "┃ 📡 NETWORK\n┃ ⬇️ HOST-DOWN ⬆️ HOST-UP\n┃ ↕️ TOTAL HOST-TOTAL\n┃ 📦 TODAY HOST-USAGE\n┃ ⚡ CPU"
	out := replacePGTraffic(text, "N/A", false)
	if strings.Contains(out, "HOST-") || !strings.Contains(out, "📦 24H  N/A") || !strings.Contains(out, "┃ ⚡ CPU") {
		t.Fatal(out)
	}
}
