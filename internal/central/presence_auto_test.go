package central

import (
	bytesbuf "bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/config"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/presence"
	"github.com/SevinEW/marzban-monitoring/internal/storage"
)

func TestAutoDiscoveryRequiresNodeAuthenticationAndUsesObservedPeer(t *testing.T) {
	d := t.TempDir()
	st, e := storage.Open(filepath.Join(d, "state.json"))
	if e != nil {
		t.Fatal(e)
	}
	st.UpsertNode(model.Node{ID: "new", Secret: "per-node-secret", Daily: map[string]model.DailyStats{}})
	s := &Server{store: st, cfg: config.Config{PresenceAutoEnroll: true, PresenceNodeIDs: []string{}}}
	call := func(valid bool) *httptest.ResponseRecorder {
		body := []byte(`{"status":"discovered","peer_ip":"203.0.113.99"}`)
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		req := httptest.NewRequest("POST", "/api/v1/presence-enroll", bytesbuf.NewReader(body))
		req.RemoteAddr = "192.0.2.8:3000"
		req.Header.Set("X-MW-Node", "new")
		req.Header.Set("X-MW-Time", stamp)
		m := hmac.New(sha256.New, []byte("per-node-secret"))
		m.Write([]byte("presence-enroll-v1\n" + stamp + "\n"))
		m.Write(body)
		sig := hex.EncodeToString(m.Sum(nil))
		if !valid {
			sig = "invalid"
		}
		req.Header.Set("X-MW-Signature", sig)
		w := httptest.NewRecorder()
		s.enrollment(w, req, filepath.Join(d, "plans.json"), filepath.Join(d, "reports"))
		return w
	}
	if w := call(false); w.Code != 401 {
		t.Fatal("unauthenticated discovery accepted")
	}
	if w := call(true); w.Code != 204 {
		t.Fatalf("new node discovery failed: %d", w.Code)
	}
	raw, e := os.ReadFile(filepath.Join(d, "reports", "new.json"))
	if e != nil {
		t.Fatal(e)
	}
	var report enrollmentReport
	if json.Unmarshal(raw, &report) != nil || report.PeerIP != "192.0.2.8" {
		t.Fatal("client forged observed peer")
	}
}

func TestAutoScopeAddsOnlyMatchedNodesWithoutHidingMissingCounts(t *testing.T) {
	st, e := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	for _, id := range []string{"old", "new", "bot"} {
		st.UpsertNode(model.Node{ID: id, Name: id, LastSeen: now, Daily: map[string]model.DailyStats{}})
	}
	s := &Server{store: st, presence: presence.NewRegistry(), cfg: config.Config{PresenceAutoEnroll: true, PresenceNodeIDs: []string{"old"}}, autoScopeNext: now.Add(time.Minute), autoScopeIDs: map[string]bool{"new": true}}
	frame := s.webSnapshot(now)
	if frame.Presence.Expected != 2 || frame.Presence.Ready != 0 || frame.Presence.Users != nil {
		t.Fatal("unverified new source hidden or counted as zero")
	}
	for _, n := range frame.Nodes {
		if n.ID == "bot" && n.Presence.Status != "not_applicable" {
			t.Fatal("non VPN badge exposed")
		}
	}
	s.cfg.PresenceAutoEnroll = false
	if s.webSnapshot(now).Presence.Expected != 1 {
		t.Fatal("auto scope used while disabled")
	}
}

func TestAutoScopeRejectsInvalidOrOversizedFiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "scope.json")
	for _, raw := range []string{`{"ids":["../bad"]}`, `{"ids":`, string(make([]byte, 32769))} {
		os.WriteFile(p, []byte(raw), 0600)
		if readAutoPresenceScope(p, time.Now()) != nil {
			t.Fatal("invalid scope accepted")
		}
	}
	os.WriteFile(p, []byte(`{"ids":["a","a","b"]}`), 0600)
	if len(readAutoPresenceScope(p, time.Now())) != 2 {
		t.Fatal("scope did not deduplicate")
	}
}
