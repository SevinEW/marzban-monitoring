package central

import (
	bytesbuf "bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/storage"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestReleaseCacheRequiresNodeAuthenticationAndWhitelistedAsset(t *testing.T) {
	dir := t.TempDir()
	st, _ := storage.Open(filepath.Join(dir, "state.json"))
	st.UpsertNode(model.Node{ID: "agent", Secret: "secret", Daily: map[string]model.DailyStats{}})
	s := &Server{store: st}
	os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte("manifest"), 0600)
	for _, tc := range []struct {
		name, domain string
		code         int
	}{{"SHA256SUMS", "release-v1\n", 200}, {"SHA256SUMS", "presence-v1\n", 401}, {"../config.json", "release-v1\n", 404}, {"marzwatch-linux-arm64", "release-v1\n", 404}} {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		body := []byte(tc.name)
		r := httptest.NewRequest("POST", "/api/v1/release", bytesbuf.NewReader(body))
		r.Header.Set("X-MW-Node", "agent")
		r.Header.Set("X-MW-Time", ts)
		m := hmac.New(sha256.New, []byte("secret"))
		m.Write([]byte(tc.domain + ts + "\n"))
		m.Write(body)
		r.Header.Set("X-MW-Signature", hex.EncodeToString(m.Sum(nil)))
		w := httptest.NewRecorder()
		s.serveRelease(w, r, dir)
		if w.Code != tc.code {
			t.Fatalf("%s: %d", tc.name, w.Code)
		}
		if tc.code == 200 && w.Body.String() != "manifest" {
			t.Fatal("wrong asset")
		}
	}
}
