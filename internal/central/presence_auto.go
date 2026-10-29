package central

import (
	"encoding/json"
	"io"
	"os"
	"time"
)

const autoPresenceScopePath = "/etc/marzwatch/presence-auto-scope.json"

// The privileged reconciler publishes only IDs, never plans or credentials.
// Reading is bounded and cached, rather than adding file IO to each live frame.
func readAutoPresenceScope(path string, now time.Time) map[string]bool {
	out := map[string]bool{}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 32769))
	var scope struct {
		IDs          []string          `json:"ids"`
		Replacements map[string]string `json:"replacements"`
	}
	if err != nil || len(raw) > 32768 || json.Unmarshal(raw, &scope) != nil || len(scope.IDs) > 100 {
		return nil
	}
	for _, id := range scope.IDs {
		if !enrollmentID.MatchString(id) {
			return nil
		}
		out[id] = true
	}
	if len(scope.Replacements) > 100 {
		return nil
	}
	for old, replacement := range scope.Replacements {
		if old == "central" || old == replacement || !enrollmentID.MatchString(old) || !out[replacement] || out[old] {
			return nil
		}
		out[old] = false
	}
	return out
}

func (s *Server) presenceScope(now time.Time) map[string]bool {
	out := map[string]bool{}
	for _, id := range s.cfg.PresenceNodeIDs {
		out[id] = true
	}
	if !s.cfg.PresenceAutoEnroll {
		return out
	}
	s.autoScopeMu.Lock()
	defer s.autoScopeMu.Unlock()
	if !now.Before(s.autoScopeNext) {
		if ids := readAutoPresenceScope(autoPresenceScopePath, now); ids != nil {
			s.autoScopeIDs = ids
		}
		s.autoScopeNext = now.Add(10 * time.Second)
	}
	// Keep known eligible nodes visible during a reconciler outage. Their metric
	// status becomes unknown/stale instead of silently shrinking the denominator.
	for id, enabled := range s.autoScopeIDs {
		out[id] = enabled
	}
	return out
}
