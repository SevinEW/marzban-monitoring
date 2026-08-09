package central

import (
	"log"
	"time"
)

func (s *Server) staleLoop() {
	// Run once shortly after startup, then periodically. Central itself is never
	// automatically removed; only registered agent nodes are eligible.
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		s.removeStaleNodes(time.Now())
		select {
		case <-t.C:
		case <-s.stop:
			return
		}
	}
}

func (s *Server) removeStaleNodes(now time.Time) {
	removed := false
	for _, n := range s.store.Nodes() {
		if n.ID == "central" || n.LastSeen.IsZero() || now.Sub(n.LastSeen) <= staleNodeTTL {
			continue
		}
		if _, ok := s.store.RemoveNode(n.ID); !ok {
			continue
		}
		s.alertsMu.Lock()
		delete(s.alerts, n.ID)
		s.alertsMu.Unlock()
		removed = true
		log.Printf("stale node auto-removed after 6h silence: %s (%s)", n.Name, n.ID)
	}
	if removed {
		if err := s.store.Flush(); err != nil {
			log.Printf("flush after stale-node cleanup: %v", err)
		}
	}
}
