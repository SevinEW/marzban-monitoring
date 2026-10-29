package storage

import (
	"fmt"
	"github.com/SevinEW/marzban-monitoring/internal/model"
)

// RemovalFamily returns explicit aliases only; IP/name similarities never grant
// permission to remove another machine. Central is always protected.
func (s *Store) RemovalFamily(id string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if id == "central" || s.nodes[id] == nil || s.aliases[id] != "" {
		return nil
	}
	out := []string{id}
	for from, to := range s.aliases {
		if to == id {
			out = append(out, from)
		}
	}
	return out
}

// DeleteIDs is durable and idempotent. The caller first persists revocation so
// a crash between the two files cannot resurrect an explicitly removed node.
func (s *Store) DeleteIDs(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	drop := map[string]bool{}
	for _, id := range ids {
		if id == "central" || id == "" {
			return fmt.Errorf("protected identity")
		}
		drop[id] = true
	}
	oldNodes, oldAliases, oldDirty := s.nodes, s.aliases, s.dirty
	s.nodes = make(map[string]*model.Node, len(oldNodes))
	s.aliases = make(map[string]string, len(oldAliases))
	for id, n := range oldNodes {
		if !drop[id] {
			s.nodes[id] = n
		}
	}
	for from, to := range oldAliases {
		if !drop[from] && !drop[to] {
			s.aliases[from] = to
		}
	}
	s.dirty = true
	if err := s.flushLocked(); err != nil {
		s.nodes, s.aliases, s.dirty = oldNodes, oldAliases, oldDirty
		return err
	}
	return nil
}
