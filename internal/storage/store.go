package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/model"
)

type diskState struct {
	Nodes map[string]*model.Node `json:"nodes"`
}

type Store struct {
	mu    sync.RWMutex
	path  string
	nodes map[string]*model.Node
	dirty bool
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, nodes: map[string]*model.Node{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var d diskState
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	if d.Nodes != nil {
		s.nodes = d.Nodes
	}
	return s, nil
}

func (s *Store) UpsertNode(n model.Node) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := n
	if cp.Daily == nil {
		cp.Daily = map[string]model.DailyStats{}
	}
	s.nodes[n.ID] = &cp
	s.dirty = true
}
func (s *Store) GetNode(id string) (model.Node, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.nodes[id]
	if !ok {
		return model.Node{}, false
	}
	return cloneNode(*n), true
}
func (s *Store) Nodes() []model.Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Node, 0, len(s.nodes))
	for _, n := range s.nodes {
		out = append(out, cloneNode(*n))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) UpdateMetric(id string, m model.Metric, loc *time.Location) (model.Node, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[id]
	if !ok {
		return model.Node{}, false
	}
	prev := n.Latest
	n.Latest = m
	n.LastSeen = m.Timestamp
	date := m.Timestamp.In(loc).Format("2006-01-02")
	ds, ok := n.Daily[date]
	if !ok {
		ds = model.NewDaily(date)
	}
	cpu := clamp(m.CPUPercent)
	ram := pct(m.MemUsed, m.MemTotal)
	disk := pct(m.DiskUsed, m.DiskTotal)
	ds.Samples++
	ds.CPUSum += cpu
	ds.RAMSum += ram
	ds.DiskSum += disk
	if cpu < ds.CPUMin {
		ds.CPUMin = cpu
	}
	if cpu > ds.CPUMax {
		ds.CPUMax = cpu
	}
	if ram < ds.RAMMin {
		ds.RAMMin = ram
	}
	if ram > ds.RAMMax {
		ds.RAMMax = ram
	}
	if disk < ds.DiskMin {
		ds.DiskMin = disk
	}
	if disk > ds.DiskMax {
		ds.DiskMax = disk
	}
	if !prev.Timestamp.IsZero() {
		if m.RXBytesTotal >= prev.RXBytesTotal {
			ds.RXBytes += m.RXBytesTotal - prev.RXBytesTotal
		}
		if m.TXBytesTotal >= prev.TXBytesTotal {
			ds.TXBytes += m.TXBytesTotal - prev.TXBytesTotal
		}
	}
	if m.RXBps > ds.PeakRXBps {
		ds.PeakRXBps = m.RXBps
	}
	if m.TXBps > ds.PeakTXBps {
		ds.PeakTXBps = m.TXBps
	}
	if m.RXBps+m.TXBps > ds.PeakTotalBps {
		ds.PeakTotalBps = m.RXBps + m.TXBps
		ds.PeakAt = m.Timestamp
	}
	n.Daily[date] = ds
	pruneDaily(n.Daily, 14)
	s.dirty = true
	return cloneNode(*n), true
}

func pruneDaily(m map[string]model.DailyStats, keep int) {
	if len(m) <= keep {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for len(keys) > keep {
		delete(m, keys[0])
		keys = keys[1:]
	}
}
func cloneNode(n model.Node) model.Node {
	cp := n
	cp.Daily = map[string]model.DailyStats{}
	for k, v := range n.Daily {
		cp.Daily[k] = v
	}
	return cp
}
func pct(a, b uint64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) * 100 / float64(b)
}
func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(diskState{Nodes: s.nodes}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0640); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.dirty = false
	return nil
}
func (s *Store) FlushLoop(stop <-chan struct{}, logf func(string, ...any)) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			if err := s.Flush(); err != nil {
				logf("state flush error: %v", err)
			}
		case <-stop:
			_ = s.Flush()
			return
		}
	}
}
