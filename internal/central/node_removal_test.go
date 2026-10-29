package central

import (
	bytebuf "bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/storage"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func removalServer(t *testing.T, online bool) *Server {
	t.Helper()
	dir := t.TempDir()
	st, e := storage.Open(filepath.Join(dir, "state.json"))
	if e != nil {
		t.Fatal(e)
	}
	last := time.Now().Add(-time.Hour)
	if online {
		last = time.Now()
	}
	st.UpsertNode(model.Node{ID: "nodeA", Secret: "secret-a", Name: "same-name", LastContact: last, Registered: last})
	st.UpsertNode(model.Node{ID: "nodeB", Secret: "secret-b", Name: "same-name", LastContact: time.Now()})
	st.UpsertNode(model.Node{ID: "central", Secret: "local"})
	s := &Server{store: st, alerts: map[string]*alertTracker{}}
	if e = s.initRemovals(filepath.Join(dir, "removals.json")); e != nil {
		t.Fatal(e)
	}
	s.removalCap["nodeA"] = time.Now()
	return s
}
func webRemove(s *Server, id, site string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "http://127.0.0.1/api/nodes/"+id+"/remove", bytebuf.NewBufferString(`{"confirm":"`+id+`","mode":"uninstall"}`))
	req.SetPathValue("id", id)
	req.Header.Set("Sec-Fetch-Site", site)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Marzwatch-Action", "remove-node")
	w := httptest.NewRecorder()
	s.removeWebNode(w, req)
	return w
}
func action(s *Server, id, secret, nonce, phase string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(map[string]string{"nonce": nonce, "phase": phase})
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("node-remove-v1\n" + stamp + "\n"))
	mac.Write(raw)
	req := httptest.NewRequest("POST", "/api/v1/node-action", bytebuf.NewReader(raw))
	req.Header.Set("X-MW-Node", id)
	req.Header.Set("X-MW-Time", stamp)
	req.Header.Set("X-MW-Signature", hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	s.removeNodeAction(w, req)
	return w
}
func TestRemovalOfflineRevokesOnlySelectedIdentity(t *testing.T) {
	s := removalServer(t, false)
	if w := webRemove(s, "nodeA", "same-origin"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, ok := s.store.GetNode("nodeA"); ok {
		t.Fatal("not removed")
	}
	if _, ok := s.store.GetNode("nodeB"); !ok {
		t.Fatal("other node removed")
	}
	if s.removals["nodeA"].Nonce != "" || s.removals["nodeA"].Secret != "" {
		t.Fatal("offline action queued")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/metrics", bytebuf.NewBufferString("{}"))
	r.Header.Set("X-MW-Node", "nodeA")
	s.metrics(w, r)
	if w.Code != 410 {
		t.Fatal("removed agent could re-register", w.Code)
	}
	// Simulate a crash after durable revocation and before state-file deletion.
	s.store.UpsertNode(model.Node{ID: "nodeA"})
	s.store.Flush()
	if e := s.initRemovals(s.removalPath); e != nil {
		t.Fatal(e)
	}
	if _, ok := s.store.GetNode("nodeA"); ok {
		t.Fatal("revoked node resurrected")
	}
}
func TestRemovalOnlineRequiresOwnFreshSignedAcknowledgement(t *testing.T) {
	s := removalServer(t, true)
	if w := webRemove(s, "nodeA", "same-origin"); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	op := s.removals["nodeA"]
	if _, ok := s.store.GetNode("nodeA"); !ok {
		t.Fatal("removed before remote completion")
	}
	if w := action(s, "nodeA", "secret-b", op.Nonce, "accept"); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := action(s, "nodeB", "secret-b", op.Nonce, "accept"); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := action(s, "nodeA", "secret-a", op.Nonce, "complete"); w.Code != 409 {
		t.Fatal("premature complete", w.Code)
	}
	for _, phase := range []string{"poll", "accept", "complete"} {
		if w := action(s, "nodeA", "secret-a", op.Nonce, phase); w.Code != 200 {
			t.Fatal(phase, w.Code, w.Body.String())
		}
	}
	if s.removals["nodeA"].Secret != "" || !s.removalRevoked("nodeA") {
		t.Fatal("completion not persisted")
	}
	if w := action(s, "nodeA", "secret-a", op.Nonce, "accept"); w.Code != 410 {
		t.Fatal("replayed action", w.Code)
	}
}
func TestRemovalRejectsCSRFMasterOldAgentExpiryAndDiskFailure(t *testing.T) {
	s := removalServer(t, true)
	for _, site := range []string{"", "cross-site", "same-site", "none"} {
		if w := webRemove(s, "nodeA", site); w.Code != 403 {
			t.Fatal(site, w.Code)
		}
	}
	if w := webRemove(s, "central", "same-origin"); w.Code != 400 {
		t.Fatal(w.Code)
	}
	delete(s.removalCap, "nodeA")
	if w := webRemove(s, "nodeA", "same-origin"); w.Code != 409 {
		t.Fatal(w.Code)
	}
	s.removalCap["nodeA"] = time.Now()
	webRemove(s, "nodeA", "same-origin")
	op := s.removals["nodeA"]
	op.Expires = time.Now().Unix() - 1
	s.removals["nodeA"] = op
	if w := action(s, "nodeA", "secret-a", op.Nonce, "accept"); w.Code != 409 {
		t.Fatal(w.Code)
	}
	s.removalPath = filepath.Join(t.TempDir(), "missing", "state")
	if w := webRemove(s, "nodeA", "same-origin"); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if _, ok := s.store.GetNode("nodeA"); !ok {
		t.Fatal("deleted on persistence error")
	}
	if _, err := os.Stat(s.removalPath); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestListOnlyConfirmationCannotBecomeRemoteUninstall(t *testing.T) {
	s := removalServer(t, true) // reconnected while the offline dialog was open
	r := httptest.NewRequest("POST", "/api/nodes/nodeA/remove", bytebuf.NewBufferString(`{"confirm":"nodeA","mode":"list-only"}`))
	r.SetPathValue("id", "nodeA")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("X-Marzwatch-Action", "remove-node")
	w := httptest.NewRecorder()
	s.removeWebNode(w, r)
	if w.Code != 200 || s.removals["nodeA"].Nonce != "" || !s.removalRevoked("nodeA") {
		t.Fatal("list-only scope widened", w.Code, w.Body.String())
	}
}
