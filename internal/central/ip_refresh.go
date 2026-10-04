package central

import (
	"github.com/SevinEW/marzban-monitoring/internal/geo"
	"log"
	"time"
)

func (s *Server) observePublicIP(id, ip string) {
	ip = geo.PublicIP(ip)
	if ip == "" {
		return
	}
	n, ok := s.store.GetNode(id)
	if !ok || n.Location.PublicIP == ip {
		return
	}
	s.store.SetPublicIP(id, ip)
	if override, ok := s.cfg.LocationOverrides[id]; ok {
		override.PublicIP = ip
		s.store.SetLocation(id, override)
	}
	if n.Location.PublicIP != "" {
		log.Printf("node %s public IP changed: %s -> %s", n.Name, n.Location.PublicIP, ip)
	}
}

func (s *Server) localIPLoop() {
	for {
		s.observePublicIP("central", geo.DetectIP())
		select {
		case <-s.stop:
			return
		case <-time.After(30 * time.Second):
		}
	}
}
