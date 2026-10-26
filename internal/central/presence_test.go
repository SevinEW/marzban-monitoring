package central

import (
	bytesbuf "bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/presence"
	"github.com/SevinEW/marzban-monitoring/internal/storage"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestPresenceAuthenticatedAndPrivate(t *testing.T) {
	st, e := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if e != nil {
		t.Fatal(e)
	}
	st.UpsertNode(model.Node{ID: "node", Secret: "test-secret", Daily: map[string]model.DailyStats{}})
	s := &Server{store: st, presence: presence.NewRegistry()}
	p := presence.Packet{Epoch: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Seq: 1, Full: true, Status: "ok", Add: []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
	body, _ := json.Marshal(p)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	makeRequest := func(domain string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/presence", bytesbuf.NewReader(body))
		r.Header.Set("X-MW-Node", "node")
		r.Header.Set("X-MW-Time", stamp)
		m := hmac.New(sha256.New, []byte("test-secret"))
		m.Write([]byte(domain + stamp + "\n"))
		m.Write(body)
		r.Header.Set("X-MW-Signature", hex.EncodeToString(m.Sum(nil)))
		w := httptest.NewRecorder()
		s.receivePresence(w, r)
		return w
	}
	if w := makeRequest(""); w.Code != 401 {
		t.Fatal("metric signature accepted as presence")
	}
	if w := makeRequest("presence-v1\n"); w.Code != 204 {
		t.Fatalf("valid request rejected: %d", w.Code)
	}
	views, total := s.presence.Snapshot([]string{"node"}, time.Now())
	if total.Users == nil || *total.Users != 1 {
		t.Fatal("presence not applied")
	}
	public, _ := json.Marshal(views)
	if bytesbuf.Contains(public, []byte(p.Add[0])) || bytesbuf.Contains(public, []byte("test-secret")) {
		t.Fatal("identity leaked")
	}
	stamp = strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)
	if w := makeRequest("presence-v1\n"); w.Code != 401 {
		t.Fatal("old timestamp accepted")
	}
}
