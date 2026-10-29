package central

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/config"
)

const nodeRemovalsPath = "/var/lib/marzwatch/node-removals.json"

type nodeRemoval struct {
	Status  string   `json:"status"`
	Nonce   string   `json:"nonce,omitempty"`
	Secret  string   `json:"secret,omitempty"`
	Expires int64    `json:"expires,omitempty"`
	IDs     []string `json:"ids"`
}
type removalView struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// removalMu serializes operator requests, signed acknowledgements, metrics and
// registrations. Small state is written only on user actions or acknowledgements.
func (s *Server) initRemovals(path string) error {
	s.removalPath = path
	s.removals = map[string]nodeRemoval{}
	s.removalCap = map[string]time.Time{}
	s.revoked = map[string]bool{}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(raw) > 4<<20 || json.Unmarshal(raw, &s.removals) != nil || s.removals == nil || len(s.removals) > 10000 {
		return fmt.Errorf("invalid node removal journal")
	}
	for id, op := range s.removals {
		if !enrollmentID.MatchString(id) || id == "central" || len(op.IDs) == 0 || len(op.IDs) > 100 {
			return fmt.Errorf("invalid removal identity")
		}
		for _, v := range op.IDs {
			if v == "central" || !enrollmentID.MatchString(v) {
				return fmt.Errorf("invalid removal family")
			}
		}
		switch op.Status {
		case "removed":
			for _, v := range op.IDs {
				s.revoked[v] = true
			}
			if err = s.store.DeleteIDs(op.IDs); err != nil {
				return err
			}
		case "pending", "accepted", "failed":
		default:
			return fmt.Errorf("invalid removal status")
		}
	}
	return nil
}
func (s *Server) saveRemoval(id string, op nodeRemoval) error {
	if s.removals == nil {
		s.removals = map[string]nodeRemoval{}
	}
	old, had := s.removals[id]
	s.removals[id] = op
	raw, err := json.Marshal(s.removals)
	if err == nil {
		var f *os.File
		f, err = os.CreateTemp(filepath.Dir(s.removalPath), ".node-removals-")
		if err == nil {
			defer os.Remove(f.Name())
			_, err = f.Write(raw)
			if err == nil {
				err = f.Sync()
			}
			ce := f.Close()
			if err == nil {
				err = ce
			}
			if err == nil {
				err = os.Rename(f.Name(), s.removalPath)
			}
		}
	}
	if err != nil {
		if had {
			s.removals[id] = old
		} else {
			delete(s.removals, id)
		}
		return err
	}
	if op.Status == "removed" {
		if s.revoked == nil {
			s.revoked = map[string]bool{}
		}
		for _, v := range op.IDs {
			s.revoked[v] = true
		}
	}
	return nil
}
func (s *Server) removalRevoked(id string) bool {
	return s.revoked[id]
}
func (s *Server) removalSnapshot(id string, now time.Time) *removalView {
	s.removalMu.Lock()
	defer s.removalMu.Unlock()
	op, ok := s.removals[id]
	if !ok {
		return nil
	}
	if op.Status == "removed" {
		return &removalView{Status: "failed", Message: "پاک‌سازی فهرست کامل نشده؛ دوباره تلاش کنید."}
	}
	status := op.Status
	if op.Expires < now.Unix() && (status == "pending" || status == "accepted") {
		return &removalView{Status: "failed", Message: "تأیید پاک‌سازی دریافت نشد؛ حذف کامل تأیید نشده است."}
	}
	return &removalView{Status: status}
}
func (s *Server) removalHeader(id string, w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Marzwatch-Actions") == "1" {
		if s.removalCap == nil {
			s.removalCap = map[string]time.Time{}
		}
		s.removalCap[id] = time.Now()
	}
	op, ok := s.removals[id]
	if ok && op.Status == "pending" && op.Expires >= time.Now().Unix() {
		w.Header().Set("X-Marzwatch-Remove", op.Nonce)
	}
}
func removalJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func removalError(w http.ResponseWriter, status int, message string) {
	removalJSON(w, status, map[string]string{"error": message})
}

func (s *Server) removeWebNode(w http.ResponseWriter, r *http.Request) {
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	// Browsers cannot forge Fetch Metadata or send this custom header cross-origin
	// without a preflight. No CORS permissions are provided by this private API.
	if r.Header.Get("Sec-Fetch-Site") != "same-origin" || r.Header.Get("X-Marzwatch-Action") != "remove-node" || media != "application/json" {
		removalError(w, 403, "درخواست حذف معتبر نیست.")
		return
	}
	id := r.PathValue("id")
	var q struct {
		Confirm string `json:"confirm"`
		Mode    string `json:"mode"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	if err != nil || len(raw) > 1024 || json.Unmarshal(raw, &q) != nil || q.Confirm != id || (q.Mode != "list-only" && q.Mode != "uninstall") || !enrollmentID.MatchString(id) || id == "central" {
		removalError(w, 400, "سرور انتخاب‌شده معتبر نیست.")
		return
	}
	s.removalMu.Lock()
	defer s.removalMu.Unlock()
	if s.removalRevoked(id) {
		if err = s.purgeRemoved(s.removals[id].IDs); err != nil {
			removalError(w, 503, "حذف هنوز کامل نشده است.")
			return
		}
		removalJSON(w, 200, map[string]string{"status": "removed", "message": "این سرور از فهرست حذف شده است."})
		return
	}
	n, ok := s.store.GetNode(id)
	if !ok || !s.store.IsActiveNode(id) {
		removalError(w, 404, "این سرور دیگر در فهرست نیست.")
		return
	}
	now := time.Now()
	op, exists := s.removals[id]
	if exists && (op.Status == "pending" || op.Status == "accepted") && op.Expires >= now.Unix() {
		removalJSON(w, 202, map[string]string{"status": "queued", "message": "درخواست حذف در حال اجراست."})
		return
	}
	ids := s.store.RemovalFamily(id)
	if len(ids) == 0 || len(s.removals) >= 10000 {
		removalError(w, 409, "حذف این رکورد فعلاً ممکن نیست.")
		return
	}
	// Always use Central receipt time, never node time or IP reachability.
	online := !n.LastContact.IsZero() && !n.LastContact.After(now) && now.Sub(n.LastContact) <= 15*time.Second
	if !online || q.Mode == "list-only" {
		if err = s.saveRemoval(id, nodeRemoval{Status: "removed", IDs: ids}); err == nil {
			err = s.purgeRemoved(ids)
		}
		if err != nil {
			removalError(w, 503, "ثبت حذف کامل نشد؛ دوباره بررسی کنید.")
			return
		}
		removalJSON(w, 200, map[string]string{"status": "removed", "message": "فقط از فهرست حذف شد. روی سرور فرمانی اجرا نشد."})
		return
	}
	if now.Sub(s.removalCap[id]) > 15*time.Second {
		removalError(w, 409, "این نود هنوز قابلیت حذف امن را دریافت نکرده؛ ابتدا به‌روزرسانی مانیتورینگ تکمیل شود.")
		return
	}
	nonce, err := config.RandomToken(24)
	if err == nil {
		err = s.saveRemoval(id, nodeRemoval{Status: "pending", Nonce: nonce, Secret: n.Secret, Expires: now.Add(5 * time.Minute).Unix(), IDs: ids})
	}
	if err != nil {
		removalError(w, 503, "ذخیرهٔ درخواست حذف ممکن نشد.")
		return
	}
	removalJSON(w, 202, map[string]string{"status": "queued", "message": "درخواست پاک‌سازی MarzWatch ثبت شد؛ منتظر تأیید همان نود هستیم."})
}
func (s *Server) purgeRemoved(ids []string) error {
	if err := s.store.DeleteIDs(ids); err != nil {
		return err
	}
	s.alertsMu.Lock()
	for _, id := range ids {
		delete(s.alerts, id)
	}
	s.alertsMu.Unlock()
	if s.host != nil {
		s.host.mu.Lock()
		for _, id := range ids {
			delete(s.host.data.Nodes, id)
		}
		s.host.mu.Unlock()
	}
	for _, id := range ids {
		os.Remove(filepath.Join(enrollmentReports, id+".json"))
	}
	return nil
}

// Fixed lifecycle protocol. A signed request can only act on that identity's
// already-confirmed operation; it never supplies shell commands or file paths.
func (s *Server) removeNodeAction(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.Header.Get("X-MW-Node")
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	var q struct {
		Nonce string `json:"nonce"`
		Phase string `json:"phase"`
	}
	if err != nil || len(raw) > 1024 || json.Unmarshal(raw, &q) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	s.removalMu.Lock()
	defer s.removalMu.Unlock()
	op, ok := s.removals[id]
	if !ok || id == "central" || op.Nonce == "" || !hmac.Equal([]byte(q.Nonce), []byte(op.Nonce)) {
		http.Error(w, "no action", 404)
		return
	}
	if op.Status == "removed" {
		if err = s.purgeRemoved(op.IDs); err != nil {
			http.Error(w, "persistence unavailable", 503)
			return
		}
		w.WriteHeader(410)
		return
	}
	stamp := r.Header.Get("X-MW-Time")
	ts, e := strconv.ParseInt(stamp, 10, 64)
	sig, e2 := hex.DecodeString(r.Header.Get("X-MW-Signature"))
	mac := hmac.New(sha256.New, []byte(op.Secret))
	mac.Write([]byte("node-remove-v1\n" + stamp + "\n"))
	mac.Write(raw)
	if e != nil || e2 != nil || op.Secret == "" || math.Abs(float64(time.Now().Unix()-ts)) > 30 || !hmac.Equal(mac.Sum(nil), sig) {
		http.Error(w, "unauthorized", 401)
		return
	}
	switch q.Phase {
	case "poll":
		if op.Status != "pending" || op.Expires < time.Now().Unix() {
			http.Error(w, "expired action", 409)
			return
		}
	case "accept":
		if op.Status != "pending" || op.Expires < time.Now().Unix() {
			http.Error(w, "expired action", 409)
			return
		}
		op.Status = "accepted"
		op.Expires = time.Now().Add(3 * time.Minute).Unix()
		err = s.saveRemoval(id, op)
	case "complete":
		if op.Status != "accepted" {
			http.Error(w, "action not accepted", 409)
			return
		}
		op.Status = "removed"
		op.Secret = ""
		op.Expires = 0
		if err = s.saveRemoval(id, op); err == nil {
			err = s.purgeRemoved(op.IDs)
		}
	case "failed":
		if op.Status != "pending" && op.Status != "accepted" {
			http.Error(w, "action not active", 409)
			return
		}
		op.Status = "failed"
		err = s.saveRemoval(id, op)
	default:
		http.Error(w, "invalid phase", 400)
		return
	}
	if err != nil {
		http.Error(w, "persistence unavailable", 503)
		return
	}
	removalJSON(w, 200, map[string]any{"action": "uninstall-marzwatch", "node_id": id, "nonce": op.Nonce, "expires": op.Expires, "status": op.Status})
}
