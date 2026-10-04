package storage

import (
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"time"
)

// Receipt time is owned by Central, not by the node's sample clock.
func (s *Store) RecordContact(id string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.nodes[id]
	if n == nil {
		return false
	}
	if s.aliases[id] != "" {
		return true
	} // retired streams cannot keep the selected node alive
	n.LastContact = now
	s.dirty = true
	return true
}

// Expire removes a canonical node and its archived aliases in one durable
// transaction. On disk failure no identity or topic is considered removed.
func (s *Store) Expire(now time.Time, ttl time.Duration) ([]model.Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var removed []model.Node
	expired := map[string]bool{}
	for id, n := range s.nodes {
		if id == "central" || s.aliases[id] != "" {
			continue
		}
		last := n.LastContact
		if last.IsZero() {
			last = n.LastSeen
		} // pre-upgrade state
		if last.IsZero() {
			last = n.Registered
		}
		if last.IsZero() || last.After(now) {
			// A legacy invalid/future timestamp gets one Central-clock observation
			// window instead of either immediate deletion or permanent immortality.
			n.LastContact = now
			s.dirty = true
			continue
		}
		if now.Sub(last) > ttl {
			expired[id] = true
		}
	}
	if len(expired) == 0 {
		return nil, s.flushLocked()
	}
	oldNodes, oldAliases, oldDirty := s.nodes, s.aliases, s.dirty
	s.nodes = make(map[string]*model.Node, len(oldNodes))
	s.aliases = make(map[string]string, len(oldAliases))
	for id, n := range oldNodes {
		if expired[id] || expired[oldAliases[id]] {
			removed = append(removed, cloneNode(*n))
			continue
		}
		s.nodes[id] = n
	}
	for from, to := range oldAliases {
		if !expired[to] {
			s.aliases[from] = to
		}
	}
	s.dirty = true
	if err := s.flushLocked(); err != nil {
		s.nodes, s.aliases, s.dirty = oldNodes, oldAliases, oldDirty
		return nil, err
	}
	return removed, nil
}
