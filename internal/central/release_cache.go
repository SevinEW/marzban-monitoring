package central

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const releaseCachePath = "/usr/local/lib/marzwatch/release-cache/current"

func (s *Server) cachedRelease(w http.ResponseWriter, r *http.Request) {
	s.serveRelease(w, r, releaseCachePath)
}
func (s *Server) serveRelease(w http.ResponseWriter, r *http.Request, root string) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.Header.Get("X-MW-Node")
	node, ok := s.store.GetNode(id)
	if !ok || id == "central" || !s.store.IsActiveNode(id) {
		http.Error(w, "unauthorized", 401)
		return
	}
	stamp := r.Header.Get("X-MW-Time")
	ts, e := strconv.ParseInt(stamp, 10, 64)
	if e != nil || math.Abs(float64(time.Now().Unix())-float64(ts)) > 30 {
		http.Error(w, "stale request", 401)
		return
	}
	body, e := io.ReadAll(io.LimitReader(r.Body, 81))
	if e != nil || len(body) > 80 {
		http.Error(w, "invalid request", 400)
		return
	}
	mac := hmac.New(sha256.New, []byte(node.Secret))
	mac.Write([]byte("release-v1\n" + stamp + "\n"))
	mac.Write(body)
	sig, e := hex.DecodeString(r.Header.Get("X-MW-Signature"))
	if e != nil || !hmac.Equal(mac.Sum(nil), sig) {
		http.Error(w, "unauthorized", 401)
		return
	}
	name := string(body)
	switch name {
	case "SHA256SUMS", "marzwatch-linux-amd64", "marzwatch-linux-arm64", "marzwatch-auto-update.sh":
	default:
		http.NotFound(w, r)
		return
	}
	f, e := os.Open(filepath.Join(root, name))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > 32<<20 {
		http.Error(w, "invalid cache", 503)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	io.Copy(w, f)
}
