package storage

import "github.com/SevinEW/marzban-monitoring/internal/model"

func (s *Store) SetLocation(id string, loc model.Location) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n, ok := s.nodes[id]; ok {
        if loc.PublicIP != "" && n.Location.PublicIP != "" && loc.PublicIP != n.Location.PublicIP { return }
		if loc.PublicIP == "" {
			loc.PublicIP = n.Location.PublicIP
		}
		n.Location = loc
		s.dirty = true
	}
}
func (s *Store) FillPublicIP(id, ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n, ok := s.nodes[id]; ok && n.Location.PublicIP == "" && ip != "" {
		n.Location.PublicIP = ip
		s.dirty = true
	}
}

// A newly detected public interface address invalidates an old egress location.
func (s *Store) SetPublicIP(id, ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n, ok := s.nodes[id]; ok && ip != "" && n.Location.PublicIP != ip {
		n.Location = model.Location{PublicIP: ip}
		s.dirty = true
	}
}
