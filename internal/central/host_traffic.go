package central

import (
	"encoding/json"
	"fmt"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

const hostTrafficPath = "/var/lib/marzwatch/host-traffic.json"
const hostWindow = 86400.0

// Minute buckets bound memory/disk use; byte deltas are collected every 5s.
// Only a window boundary bucket is prorated. Never import calendar-day totals.
type hostBucket struct {
	Start, End float64
	RX, TX     float64
}
type hostNode struct {
	First, Last, Gap float64
	Metric           time.Time
	RX, TX, Uptime   uint64
	Buckets          []hostBucket
}
type hostDisk struct {
	Version int
	Nodes   map[string]*hostNode
}
type hostTraffic struct {
	mu   sync.Mutex
	path string
	data hostDisk
}

func openHostTraffic(path string) (*hostTraffic, error) {
	h := &hostTraffic{path: path, data: hostDisk{Version: 1, Nodes: map[string]*hostNode{}}}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return h, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 32<<20))
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &h.data); err != nil {
		return nil, err
	}
	if h.data.Version != 1 || h.data.Nodes == nil {
		return nil, fmt.Errorf("invalid host traffic history")
	}
	return h, nil
}

func (h *hostTraffic) sample(nodes []model.Node, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	t := float64(now.UnixNano()) / 1e9
	for _, n := range nodes {
		m := n.Latest
		contact := n.LastContact
		if n.ID == "central" {
			contact = m.Timestamp
		}
		if m.Timestamp.IsZero() || contact.IsZero() || now.Sub(contact) > 30*time.Second || contact.After(now.Add(5*time.Second)) {
			continue
		}
		p := h.data.Nodes[n.ID]
		if p == nil {
			h.data.Nodes[n.ID] = &hostNode{First: t, Last: t, Gap: t, Metric: m.Timestamp, RX: m.RXBytesTotal, TX: m.TXBytesTotal, Uptime: m.UptimeSeconds}
			continue
		}
		if !m.Timestamp.After(p.Metric) || t <= p.Last {
			continue
		}
		reset := m.UptimeSeconds < p.Uptime || m.RXBytesTotal < p.RX || m.TXBytesTotal < p.TX
		if reset {
			p.Gap = t
		} else {
			if t-p.Last > 30 {
				p.Gap = t
			}
			rx, tx := float64(m.RXBytesTotal-p.RX), float64(m.TXBytesTotal-p.TX)
			start := p.Last
			if start < t-hostWindow-3600 {
				start = t - hostWindow - 3600
			}
			for start < t {
				end := float64((int64(start)/60 + 1) * 60)
				if end > t {
					end = t
				}
				share := (end - start) / (t - p.Last)
				b := hostBucket{Start: start, End: end, RX: rx * share, TX: tx * share}
				size := len(p.Buckets)
				if size > 0 && int64(p.Buckets[size-1].Start)/60 == int64(start)/60 && p.Buckets[size-1].End == start {
					q := &p.Buckets[size-1]
					q.End = end
					q.RX += b.RX
					q.TX += b.TX
				} else {
					p.Buckets = append(p.Buckets, b)
				}
				start = end
			}
		}
		p.Last = t
		p.Metric = m.Timestamp
		p.RX = m.RXBytesTotal
		p.TX = m.TXBytesTotal
		p.Uptime = m.UptimeSeconds
	}
	for id, p := range h.data.Nodes {
		if t-p.Last > hostWindow+3600 {
			delete(h.data.Nodes, id)
			continue
		}
		i := 0
		for i < len(p.Buckets) && p.Buckets[i].End < t-hostWindow-60 {
			i++
		}
		if i > 0 {
			p.Buckets = append([]hostBucket(nil), p.Buckets[i:]...)
		}
	}
}

func hostSum(p *hostNode, t float64) (float64, bool) {
	if p == nil {
		return 0, false
	}
	cut := t - hostWindow
	total := 0.0
	for _, b := range p.Buckets {
		if b.End <= cut || b.Start >= t || b.End <= b.Start {
			continue
		}
		start, end := b.Start, b.End
		if start < cut {
			start = cut
		}
		if end > t {
			end = t
		}
		total += (b.RX + b.TX) * (end - start) / (b.End - b.Start)
	}
	return total, p.First <= cut && p.Gap <= cut && t-p.Last <= 30
}

func (h *hostTraffic) value(id string, now time.Time) string {
	if h == nil {
		return "N/A"
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	t := float64(now.UnixNano()) / 1e9
	total := 0.0
	complete := true
	if id != "" {
		p := h.data.Nodes[id]
		if p == nil {
			return "N/A"
		}
		total, complete = hostSum(p, t)
	} else {
		if len(h.data.Nodes) == 0 {
			return "N/A"
		}
		for _, p := range h.data.Nodes {
			v, ok := hostSum(p, t)
			total += v
			complete = complete && ok
		}
	}
	value := bytes(uint64(total))
	if !complete {
		value += " *"
	}
	return value
}

func (h *hostTraffic) save() error {
	h.mu.Lock()
	b, err := json.Marshal(h.data)
	h.mu.Unlock()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(h.path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(h.path+".tmp", h.path)
}
func (s *Server) hostTrafficLoop() {
	if s.host == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	lastSave := time.Time{}
	for {
		now := time.Now()
		s.host.sample(s.store.Nodes(), now)
		if now.Sub(lastSave) >= 30*time.Second {
			if err := s.host.save(); err != nil {
				log.Printf("host traffic save: %v", err)
			}
			lastSave = now
		}
		select {
		case <-ticker.C:
		case <-s.stop:
			_ = s.host.save()
			return
		}
	}
}
func (s *Server) formatHostNode(n model.Node, on bool, date string) string {
	return replacePGTraffic(formatNodeBlock(n, on, date), s.host.value(n.ID, time.Now()), on)
}
func (s *Server) formatHostOverview(nodes []model.Node, now time.Time) string {
	return replacePGTraffic(formatOverviewLive(nodes, now), s.host.value("", now), true)
}
