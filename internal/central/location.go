package central

import (
	"github.com/SevinEW/marzban-monitoring/internal/geo"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"net"
	"strings"
	"time"
)

func peerPublicIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return geo.PublicIP(host)
}

func (s *Server) locationLoop() {
	next := map[string]time.Time{}
	lastIP := map[string]string{}
	failures := map[string]int{}
	for {
		for _, n := range s.store.Nodes() {
			if time.Now().Before(next[n.ID]) && lastIP[n.ID] == n.Location.PublicIP {
				continue
			}
			if lastIP[n.ID] != n.Location.PublicIP {
				failures[n.ID] = 0
			}
			lastIP[n.ID] = n.Location.PublicIP
			next[n.ID] = time.Now().Add(15 * time.Minute)
			loc := n.Location
			if override, ok := s.cfg.LocationOverrides[n.ID]; ok {
				override.PublicIP = loc.PublicIP
				s.store.SetLocation(n.ID, override)
				next[n.ID] = time.Now().Add(24 * time.Hour)
				continue
			}
			if geo.PublicIP(loc.PublicIP) == "" {
				next[n.ID] = time.Now().Add(time.Minute)
				continue
			}
			detected, available := geo.Resolve(loc.PublicIP)
			if available {
				s.store.SetLocation(n.ID, detected)
			}
			if detected.CountryCode != "" {
				next[n.ID] = time.Now().Add(24 * time.Hour)
				failures[n.ID] = 0
			} else {
				failures[n.ID]++
				if failures[n.ID] == 2 {
					next[n.ID] = time.Now().Add(time.Hour)
				}
				if failures[n.ID] >= 3 {
					next[n.ID] = time.Now().Add(6 * time.Hour)
				}
			}
		}
		live := map[string]bool{}
		for _, n := range s.store.Nodes() {
			live[n.ID] = true
		}
		for id := range next {
			if !live[id] {
				delete(next, id)
				delete(lastIP, id)
				delete(failures, id)
			}
		}
		select {
		case <-s.stop:
			return
		case <-time.After(time.Minute):
		}
	}
}
func countryKey(n model.Node) string {
	cc := strings.ToUpper(strings.TrimSpace(n.Location.CountryCode))
	if len(cc) != 2 {
		return "ZZZ"
	}
	return cc
}
