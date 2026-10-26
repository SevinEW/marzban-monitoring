package central

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/config"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/presence"
	"github.com/SevinEW/marzban-monitoring/internal/storage"
)

func TestPresenceScopePreservesHostMetricsAndExcludesNonVPNCounts(t *testing.T) {
	st, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, id := range []string{"vpn", "bot"} {
		st.UpsertNode(model.Node{ID: id, Name: id, LastSeen: now, Daily: map[string]model.DailyStats{}})
	}
	r := presence.NewRegistry()
	for _, id := range []string{"vpn", "bot"} {
		if err := r.Apply(id, presence.Packet{Epoch: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Seq: 1, Full: true,
			Status: "ok", Add: []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}, now); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{store: st, presence: r, cfg: config.Config{PresenceNodeIDs: []string{"vpn", "vpn", "missing"}}}
	f := s.webSnapshot(now)
	if len(f.Nodes) != 2 || f.Presence.Expected != 1 || f.Presence.Ready != 1 || f.Presence.Users == nil || *f.Presence.Users != 1 {
		t.Fatal("scope changed host inventory or inflated presence coverage")
	}
	for _, n := range f.Nodes {
		if n.ID == "bot" && (n.Presence.Status != "not_applicable" || n.Presence.Users != nil) {
			t.Fatal("non-VPN counter exposed")
		}
	}
	s.cfg.PresenceNodeIDs = []string{}
	f = s.webSnapshot(now)
	if f.Presence.Expected != 0 || f.Presence.Users != nil || len(f.Nodes) != 2 {
		t.Fatal("empty scope became all nodes or false zero")
	}
	s.cfg.PresenceNodeIDs = nil
	if s.webSnapshot(now).Presence.Expected != 2 {
		t.Fatal("legacy scope lost")
	}
}

func TestEmptyPresenceScopeSurvivesConfigSerialization(t *testing.T) {
	for _, ids := range [][]string{nil, {}} {
		b, err := json.Marshal(config.Config{PresenceNodeIDs: ids})
		if err != nil {
			t.Fatal(err)
		}
		var c config.Config
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatal(err)
		}
		if (ids == nil) != (c.PresenceNodeIDs == nil) {
			t.Fatal("explicit empty scope lost")
		}
	}
}
