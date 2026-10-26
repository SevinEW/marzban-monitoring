package storage

// IsActiveNode excludes archived aliases without copying node histories.
func (s *Store) IsActiveNode(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nodes[id] != nil && s.canonicalLocked(id) == id
}
