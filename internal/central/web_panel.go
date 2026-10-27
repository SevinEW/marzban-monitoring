package central

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
"github.com/SevinEW/marzban-monitoring/internal/presence"
)

//go:embed web/*
var webAssets embed.FS

// Explicit DTO: never serialize model.Node (contains registration secrets).
type webNode struct {
 Presence presence.View `json:"presence"`
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Country          string  `json:"country"`
	Code             string  `json:"code"`
	City             string  `json:"city"`
	IP               string  `json:"ip"`
	OS               string  `json:"os"`
	Cores            int     `json:"cores"`
	CPU              float64 `json:"cpu"`
	Load             float64 `json:"load"`
	RAM              uint64  `json:"ram"`
	RAMTotal         uint64  `json:"ram_total"`
	Disk             uint64  `json:"disk"`
	DiskTotal        uint64  `json:"disk_total"`
	RX               float64 `json:"rx"`
	TX               float64 `json:"tx"`
	Uptime           uint64  `json:"uptime"`
	Seen             int64   `json:"seen"`
	Status           string  `json:"status"`
	Traffic          float64 `json:"traffic"`
	Complete         bool    `json:"complete"`
	TrafficAvailable bool    `json:"traffic_available"`
}
type webFrame struct {
 Presence presence.Summary `json:"presence"`
	Time  int64     `json:"time"`
	Nodes []webNode `json:"nodes"`
}
type webHub struct {
	mu      sync.RWMutex
	latest  []byte
	history [][]byte
	clients chan struct{}
	changed chan struct{}
}

func (s *Server) webSnapshot(now time.Time) webFrame {
	f := webFrame{Time: now.UnixMilli(), Nodes: []webNode{}}
	nodes:=sortedForumNodes(s.store.Nodes())
 eligible:=s.presenceScope(now)
 ids:=make([]string,0,len(nodes));for _,n:=range nodes{if (s.cfg.PresenceNodeIDs==nil&&!s.cfg.PresenceAutoEnroll)||eligible[n.ID]{ids=append(ids,n.ID)}}
 registry:=s.presence;if registry==nil{registry=presence.NewRegistry()}
 views,summary:=registry.SnapshotWithQuality(ids,now,s.cfg.PresenceQualityBlocks);f.Presence=summary
 for _, n := range nodes {
		m := n.Latest
		w := webNode{ID: n.ID, Name: n.Name, Country: n.Location.Country, Code: n.Location.CountryCode, City: n.Location.City, IP: n.Location.PublicIP, OS: n.OS, Cores: n.Cores, CPU: m.CPUPercent, Load: m.Load1, RAM: m.MemUsed, RAMTotal: m.MemTotal, Disk: m.DiskUsed, DiskTotal: m.DiskTotal, RX: m.RXBps, TX: m.TXBps, Uptime: m.UptimeSeconds, Seen: n.LastSeen.UnixMilli(), Status: "online"}
		age := now.Sub(n.LastSeen)
		if n.LastSeen.IsZero() || age > 150*time.Second {
			w.Status = "offline"
			w.RX = 0
			w.TX = 0
		} else if age > 10*time.Second {
			w.Status = "stale"
		} else if m.CPUPercent >= 90 || pct(m.MemUsed, m.MemTotal) >= 90 || pct(m.DiskUsed, m.DiskTotal) >= 90 {
			w.Status = "warning"
		}
		if s.host != nil {
			s.host.mu.Lock()
			p := s.host.data.Nodes[n.ID]
			w.TrafficAvailable = p != nil
			w.Traffic, w.Complete = hostSum(p, float64(now.UnixNano())/1e9)
			s.host.mu.Unlock()
		}
		if v,ok:=views[n.ID];ok{w.Presence=v}else{w.Presence=presence.View{Status:"not_applicable"}}
 f.Nodes = append(f.Nodes, w)
	}
	return f
}
func (h *webHub) publish(f webFrame) {
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.latest = b
	// Six minutes of 1-second samples, shared by every viewer. No disk writes.
	if len(h.history) >= 360 {
		copy(h.history, h.history[1:])
		h.history = h.history[:359]
	}
	h.history = append(h.history, b)
	if h.changed != nil {
		close(h.changed)
	}
	h.changed = make(chan struct{})
}
func (h *webHub) stream(w http.ResponseWriter, r *http.Request) {
	select {
	case h.clients <- struct{}{}:
		defer func() { <-h.clients }()
	default:
		http.Error(w, "Too many viewers", 503)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	ctl := http.NewResponseController(w)
	send := func(event string, b []byte) error {
		_ = ctl.SetWriteDeadline(time.Now().Add(8 * time.Second))
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return err
		}
		return ctl.Flush()
	}
	h.mu.RLock()
	history := append([][]byte(nil), h.history...)
	h.mu.RUnlock()
	raw := make([]json.RawMessage, len(history))
	for i, b := range history {
		raw[i] = b
	}
	b, _ := json.Marshal(raw)
	if send("history", b) != nil {
		return
	}
	for {
		h.mu.RLock()
		b = h.latest
		changed := h.changed
		h.mu.RUnlock()
		if send("snapshot", b) != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		}
	}
}
func (s *Server) startWebPanel() error {
	if s.cfg.WebListen == "" {
		return nil
	}
	host, _, err := net.SplitHostPort(s.cfg.WebListen)
	if err != nil {
		return fmt.Errorf("web listen: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("web panel must bind to loopback; use an authenticated TLS reverse proxy or SSH tunnel")
	}
	ln, err := net.Listen("tcp", s.cfg.WebListen)
	if err != nil {
		return err
	}
	h := &webHub{clients: make(chan struct{}, 16)}
	h.publish(s.webSnapshot(time.Now()))
	assets, _ := fs.Sub(webAssets, "web")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stream", h.stream)
	mux.HandleFunc("GET /api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h.mu.RLock()
		b := h.latest
		h.mu.RUnlock()
		_, _ = w.Write(b)
	})
	mux.Handle("GET /", http.FileServer(http.FS(assets)))
	handler := webPanelSecurity(mux)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10}
	go func() {
		if e := server.Serve(ln); e != nil && e != http.ErrServerClosed {
			log.Printf("web panel: %v", e)
		}
	}()
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				_ = server.Close()
				return
			case <-t.C:
				h.publish(s.webSnapshot(time.Now()))
			}
		}
	}()
	log.Printf("Private web panel listening on %s", s.cfg.WebListen)
	return nil
}
