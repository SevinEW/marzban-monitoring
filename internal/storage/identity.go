package storage

import (
	"fmt"
	"strings"

	"github.com/SevinEW/marzban-monitoring/internal/model"
)

// Aliases are explicit operator decisions, never guesses from a name or IP.
// Archived nodes retain their credentials and history for authenticated late
// packets, but never appear as additional servers in a snapshot.
func (s *Store) ApplyAliases(aliases map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]string, len(s.aliases)+len(aliases))
	for from, to := range s.aliases {
		next[from] = to
	}
	for from, to := range aliases {
		next[from] = to
	}
	for from, to := range next {
		if from == "central" || to == "central" || from == to || from == "" || to == "" {
			return fmt.Errorf("invalid node alias")
		}
		if s.nodes[from] == nil || s.nodes[to] == nil {
			return fmt.Errorf("alias references an unknown node")
		}
		if _, chained := next[to]; chained {
			return fmt.Errorf("alias chains are not allowed")
		}
		a, b := s.nodes[from], s.nodes[to]
		if a.ServerKey != "" && b.ServerKey != "" && a.ServerKey != b.ServerKey {
			return fmt.Errorf("nodes have different machine identities")
		}
	}
	s.aliases = next
	s.dirty = true
	return nil
}

func (s *Store) canonicalLocked(id string) string {
	if target := s.aliases[id]; target != "" {
		return target
	}
	return id
}

func ValidServerKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	return strings.Trim(key, "0123456789abcdef") == ""
}

// Register is atomic with persistence: retrying an unanswered registration
// returns the same identity, including after a Central restart.
func (s *Store) Register(n model.Node) (model.Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n.ServerKey != "" && !ValidServerKey(n.ServerKey) {
		return model.Node{}, fmt.Errorf("invalid machine identity")
	}
	if n.ServerKey != "" {
		for id, old := range s.nodes {
			if old.ServerKey != n.ServerKey {
				continue
			}
			target := s.nodes[s.canonicalLocked(id)]
			if err := s.flushLocked(); err != nil {
				return model.Node{}, err
			}
			return cloneNode(*target), nil
		}
	}
	if n.ID == "" || n.Secret == "" || s.nodes[n.ID] != nil {
		return model.Node{}, fmt.Errorf("invalid new node identity")
	}
	if n.Daily == nil {
		n.Daily = map[string]model.DailyStats{}
	}
	s.nodes[n.ID] = &n
	s.dirty = true
	if err := s.flushLocked(); err != nil {
		return model.Node{}, err
	}
	return cloneNode(n), nil
}

// BindServerKey is called only after the metric's existing HMAC is verified.
// Never silently combine two independently registered identities on collision.
func (s *Store) BindServerKey(id, key string) error {
	if key == "" {
		return nil
	} // older agents
	if !ValidServerKey(key) {
		return fmt.Errorf("invalid machine identity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aliases[id] != "" {
		return nil
	} // retired stream cannot bind/replace the chosen identity
	targetID := s.canonicalLocked(id)
	target := s.nodes[targetID]
	if target == nil {
		return fmt.Errorf("unknown node")
	}
	if target.ServerKey == key {
		return nil
	}
	if target.ServerKey != "" {
		return fmt.Errorf("machine identity changed")
	}
	for otherID, n := range s.nodes {
		if n.ServerKey == key && s.canonicalLocked(otherID) != targetID {
			return fmt.Errorf("machine identity already belongs to another node; explicit merge required")
		}
	}
	target.ServerKey = key
	s.dirty = true
	return s.flushLocked()
}
