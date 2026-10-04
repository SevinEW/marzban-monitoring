package central

import (
	bytebuf "bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/SevinEW/marzban-monitoring/internal/config"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/storage"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAuthenticatedMetricRefreshesAddressAndReceiptOnly(t *testing.T) {
	st, _ := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	now := time.Now()
	st.UpsertNode(model.Node{ID: "node", Secret: "test-key", LastContact: now.Add(-2 * time.Hour), Latest: model.Metric{Timestamp: now}, Location: model.Location{PublicIP: "8.8.8.8"}})
	s := &Server{store: st, tz: time.UTC}
	body, _ := json.Marshal(model.Metric{PublicIP: "1.1.1.1", Timestamp: now.Add(-time.Second)})
	ts := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, []byte("test-key"))
	mac.Write([]byte(ts + "\n"))
	mac.Write(body)
	for _, valid := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/api/v1/metrics", bytebuf.NewReader(body))
		r.Header.Set("X-MW-Node", "node")
		r.Header.Set("X-MW-Time", ts)
		sig := "bad"
		if valid {
			sig = hex.EncodeToString(mac.Sum(nil))
		}
		r.Header.Set("X-MW-Signature", sig)
		w := httptest.NewRecorder()
		s.metrics(w, r)
		n, _ := st.GetNode("node")
		if !valid {
			if w.Code != 401 || n.Location.PublicIP != "8.8.8.8" || !n.LastContact.Before(now) {
				t.Fatal("unauthenticated request changed receipt/IP")
			}
		} else if w.Code != 204 || n.Location.PublicIP != "1.1.1.1" || n.LastContact.Before(now) {
			t.Fatalf("valid receipt not recorded: %d", w.Code)
		}
	}
	if removed, err := st.Expire(now, time.Hour); err != nil || len(removed) != 0 {
		t.Fatal("connected node removed after clock correction")
	}
}

func TestChangedIPInvalidatesOldGeoButKeepsCountryOverride(t *testing.T) {
	st, _ := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	st.UpsertNode(model.Node{ID: "a", Location: model.Location{PublicIP: "8.8.8.8", CountryCode: "GB", City: "old"}})
	s := &Server{store: st, cfg: config.Config{LocationOverrides: map[string]model.Location{"a": {CountryCode: "TR", Country: "Turkey"}}}}
	s.observePublicIP("a", "1.1.1.1")
	n, _ := st.GetNode("a")
	if n.Location.PublicIP != "1.1.1.1" || n.Location.CountryCode != "TR" || n.Location.City != "" {
		t.Fatalf("location=%+v", n.Location)
	}
	s.observePublicIP("a", "")
	n, _ = st.GetNode("a")
	if n.Location.PublicIP != "1.1.1.1" {
		t.Fatal("empty result erased valid address")
	}
	// An old location request finishing late must not restore the previous IP.
	st.SetLocation("a", model.Location{PublicIP: "8.8.8.8", CountryCode: "GB"})
	n, _ = st.GetNode("a")
	if n.Location.CountryCode != "TR" {
		t.Fatal("stale lookup overwrote current location")
	}
}
