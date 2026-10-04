package agent

import (
	"github.com/SevinEW/marzban-monitoring/internal/geo"
	"log"
	"time"
)

func (a *Agent) refreshPublicIP(detect func() string) {
	ip := geo.PublicIP(detect())
	if ip == "" {
		return
	}
	previous := ""
	if old := a.publicIP.Load(); old != nil {
		previous = old.(string)
	}
	if previous == ip {
		return
	}
	a.publicIP.Store(ip)
	if previous != "" {
		log.Printf("public IP changed: %s -> %s", previous, ip)
	}
}

func (a *Agent) publicIPLoop() {
	for {
		a.refreshPublicIP(geo.DetectIP)
		time.Sleep(30 * time.Second)
	}
}
