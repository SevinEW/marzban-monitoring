package central

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

const enrollmentPlans = "/etc/marzwatch/presence-fleet-plans.json"
const enrollmentReports = "/var/lib/marzwatch/presence-enrollment"

type enrollmentReport struct {
	Status    string          `json:"status"`
	Reason    string          `json:"reason,omitempty"`
	Inventory json.RawMessage `json:"inventory,omitempty"`
	Proof     json.RawMessage `json:"proof,omitempty"`
	Received  int64           `json:"received"`
}
type enrollmentPlanFile struct {
	Expires int64                      `json:"expires"`
	Plans   map[string]json.RawMessage `json:"plans"`
}

var enrollmentID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

func (s *Server) enrollPresence(w http.ResponseWriter, r *http.Request) {
	s.enrollment(w, r, enrollmentPlans, enrollmentReports)
}

// A declarative, per-node plan over the existing pinned monitoring transport.
// No shell commands, database queries, or shared join-token authorization here.
func (s *Server) enrollment(w http.ResponseWriter, r *http.Request, plansPath, reportsPath string) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.Header.Get("X-MW-Node")
	node, ok := s.store.GetNode(id)
	if !ok || id == "central" || !enrollmentID.MatchString(id) || !s.store.IsActiveNode(id) {
		http.Error(w, "unauthorized", 401)
		return
	}
	allowed := false
	for _, v := range s.cfg.PresenceNodeIDs {
		if v == id {
			allowed = true
		}
	}
	if !allowed {
		http.Error(w, "not enrolled", 403)
		return
	}
	stamp := r.Header.Get("X-MW-Time")
	ts, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || math.Abs(float64(time.Now().Unix())-float64(ts)) > 30 {
		http.Error(w, "stale request", 401)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	if err != nil || len(body) > 16384 {
		http.Error(w, "invalid request", 400)
		return
	}
	mac := hmac.New(sha256.New, []byte(node.Secret))
	mac.Write([]byte("presence-enroll-v1\n" + stamp + "\n"))
	mac.Write(body)
	sig, err := hex.DecodeString(r.Header.Get("X-MW-Signature"))
	if err != nil || !hmac.Equal(mac.Sum(nil), sig) {
		http.Error(w, "unauthorized", 401)
		return
	}
	var report enrollmentReport
	if json.Unmarshal(body, &report) != nil || len(report.Status) > 40 || len(report.Reason) > 100 {
		http.Error(w, "invalid report", 400)
		return
	}
	// One small latest-status file per authenticated, allowlisted node. No history
	// or identities are persisted. Rate limiting also bounds authenticated replays.
	s.enrollmentMu.Lock()
	defer s.enrollmentMu.Unlock()
	if s.enrollmentSeen == nil {
		s.enrollmentSeen = map[string]time.Time{}
	}
	if time.Since(s.enrollmentSeen[id]) < 5*time.Second {
		w.WriteHeader(429)
		return
	}
	s.enrollmentSeen[id] = time.Now()
	report.Received = time.Now().Unix()
	if err = os.MkdirAll(reportsPath, 0700); err != nil {
		http.Error(w, "storage unavailable", 503)
		return
	}
	raw, _ := json.Marshal(report)
	tmp, err := os.CreateTemp(reportsPath, ".report-")
	if err != nil {
		http.Error(w, "storage unavailable", 503)
		return
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(raw); err != nil {
		tmp.Close()
		http.Error(w, "storage unavailable", 503)
		return
	}
	tmp.Close()
	if err = os.Rename(tmp.Name(), filepath.Join(reportsPath, id+".json")); err != nil {
		http.Error(w, "storage unavailable", 503)
		return
	}
	f, err := os.Open(plansPath)
	if err != nil {
		w.WriteHeader(204)
		return
	}
	defer f.Close()
	raw, err = io.ReadAll(io.LimitReader(f, 1048577))
	var plans enrollmentPlanFile
	if err != nil || len(raw) > 1048576 || json.Unmarshal(raw, &plans) != nil || plans.Expires < time.Now().Unix() {
		w.WriteHeader(204)
		return
	}
	plan := plans.Plans[id]
	if len(plan) == 0 {
		w.WriteHeader(204)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(plan)
}
