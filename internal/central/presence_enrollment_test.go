package central

import (
	bytesbuf "bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/SevinEW/marzban-monitoring/internal/config"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/storage"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestEnrollmentIsScopedAuthenticatedAndNeverReturnsOtherPlans(t *testing.T) {
	dir := t.TempDir()
	st, e := storage.Open(filepath.Join(dir, "state.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"a", "b", "not-vpn"} {
		st.UpsertNode(model.Node{ID: id, Secret: "secret-" + id, Daily: map[string]model.DailyStats{}})
	}
	s := &Server{store: st, cfg: config.Config{PresenceNodeIDs: []string{"a", "b"}}}
	plans := filepath.Join(dir, "plans.json")
	reports := filepath.Join(dir, "reports")
	raw, _ := json.Marshal(map[string]any{"expires": time.Now().Unix() + 600, "plans": map[string]any{"a": map[string]string{"namespace": "only-a"}, "b": map[string]string{"namespace": "only-b"}}})
	os.WriteFile(plans, raw, 0600)
	call := func(id, domain string, stamp int64) *httptest.ResponseRecorder {
		body := []byte(`{"status":"discovered"}`)
		ts := strconv.FormatInt(stamp, 10)
		req := httptest.NewRequest("POST", "/api/v1/presence-enroll", bytesbuf.NewReader(body))
		req.Header.Set("X-MW-Node", id)
		req.Header.Set("X-MW-Time", ts)
		m := hmac.New(sha256.New, []byte("secret-"+id))
		m.Write([]byte(domain + ts + "\n"))
		m.Write(body)
		req.Header.Set("X-MW-Signature", hex.EncodeToString(m.Sum(nil)))
		w := httptest.NewRecorder()
		s.enrollment(w, req, plans, reports)
		return w
	}
	now := time.Now().Unix()
	if w := call("a", "presence-v1\n", now); w.Code != 401 {
		t.Fatal("wrong auth domain accepted")
	}
	if w := call("a", "presence-enroll-v1\n", now-60); w.Code != 401 {
		t.Fatal("stale auth accepted")
	}
	if w := call("not-vpn", "presence-enroll-v1\n", now); w.Code != 403 {
		t.Fatal("non VPN node enrolled")
	}
	w := call("a", "presence-enroll-v1\n", now)
	if w.Code != 200 || bytesbuf.Contains(w.Body.Bytes(), []byte("only-b")) || !bytesbuf.Contains(w.Body.Bytes(), []byte("only-a")) {
		t.Fatalf("plan isolation: %d %s", w.Code, w.Body.String())
	}
	if w = call("a", "presence-enroll-v1\n", now); w.Code != 429 {
		t.Fatal("unbounded replay writes")
	}
	s.enrollmentSeen = nil
	os.WriteFile(plans, []byte(`{"expires":1,"plans":{"a":{"namespace":"old"}}}`), 0600)
	if w = call("a", "presence-enroll-v1\n", now); w.Code != 204 || w.Body.Len() != 0 {
		t.Fatal("expired plan served")
	}
}
