// Package presence keeps short-lived, pseudonymous Xray account presence in RAM.
// It never counts sockets or IP addresses as physical devices.
package presence

import (
	"errors"
	"sort"
	"sync"
	"time"
)

const MaxUsers = 50000
const MaxBody = 4 << 20
const FreshFor = 6 * time.Second

var ErrResync = errors.New("presence resync required")

type Packet struct {
	Epoch  string   `json:"epoch"`
	Seq    uint64   `json:"seq"`
	Full   bool     `json:"full"`
	Status string   `json:"status"`
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}
type State struct {
	Epoch   string
	Seq     uint64
	Users   map[string]struct{}
	Status  string
	Seen    time.Time
	Retired []string
}
type Registry struct {
	mu    sync.Mutex
	nodes map[string]*State
}
type View struct {
	Users   *int   `json:"users"`
	Devices *int   `json:"devices"`
	Status  string `json:"status"`
}
type Summary struct {
	View
	ObservedUsers *int `json:"observed_users"`
	Ready         int  `json:"ready"`
	Expected      int  `json:"expected"`
}

func NewRegistry() *Registry { return &Registry{nodes: map[string]*State{}} }
func hexID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func validStatus(s string) bool {
	switch s {
	case "ok", "unconfigured", "source_changed", "unavailable", "timeout", "unsupported":
		return true
	}
	return false
}
func (r *Registry) Apply(id string, p Packet, now time.Time) error {
	if !hexID(p.Epoch) || p.Seq == 0 || !validStatus(p.Status) || len(p.Add) > MaxUsers || len(p.Remove) > MaxUsers {
		return errors.New("invalid presence packet")
	}
	if p.Status != "ok" && (len(p.Add) > 0 || len(p.Remove) > 0 || !p.Full) {
		return errors.New("invalid unavailable packet")
	}
	seen := map[string]bool{}
	for _, list := range [][]string{p.Add, p.Remove} {
		for _, u := range list {
			if !hexID(u) || seen[u] {
				return errors.New("invalid presence identity")
			}
			seen[u] = true
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old := r.nodes[id]
	if old != nil {
		if old.Epoch == p.Epoch && p.Seq <= old.Seq {
			if p.Seq == old.Seq {
				return nil
			}
			return ErrResync
		}
		for _, e := range old.Retired {
			if e == p.Epoch {
				return ErrResync
			}
		}
	}
	if !p.Full && (old == nil || old.Epoch != p.Epoch || p.Seq != old.Seq+1 || old.Status != "ok") {
		return ErrResync
	}
	if p.Full && len(p.Remove) > 0 {
		return errors.New("full packet contains removals")
	}
	if !p.Full && len(p.Add) == 0 && len(p.Remove) == 0 {
		old.Seq = p.Seq
		old.Seen = now
		return nil
	}
	users := map[string]struct{}{}
	if !p.Full {
		for u := range old.Users {
			users[u] = struct{}{}
		}
	}
	for _, u := range p.Remove {
		if _, ok := users[u]; !ok {
			return ErrResync
		}
		delete(users, u)
	}
	for _, u := range p.Add {
		if _, ok := users[u]; ok && !p.Full {
			return ErrResync
		}
		users[u] = struct{}{}
	}
	if len(users) > MaxUsers {
		return errors.New("presence too large")
	}
	total := len(users)
	for key, n := range r.nodes {
		if key != id {
			if now.Sub(n.Seen) > 5*time.Minute {
				delete(r.nodes, key)
			} else {
				total += len(n.Users)
			}
		}
	}
	if total > 250000 || (old == nil && len(r.nodes) >= 256) {
		return errors.New("presence capacity reached")
	}
	state := &State{Epoch: p.Epoch, Seq: p.Seq, Users: users, Status: p.Status, Seen: now}
	if old != nil {
		state.Retired = append(state.Retired, old.Retired...)
		if old.Epoch != p.Epoch {
			state.Retired = append(state.Retired, old.Epoch)
		}
		if len(state.Retired) > 8 {
			state.Retired = state.Retired[len(state.Retired)-8:]
		}
	}
	r.nodes[id] = state
	return nil
}
func ptr(n int) *int { return &n }
func view(n *State, now time.Time) View {
	if n == nil {
		return View{Status: "unconfigured"}
	}
	if now.Before(n.Seen) || now.Sub(n.Seen) > FreshFor {
		return View{Status: "stale"}
	}
	v := View{Status: n.Status}
	if n.Status == "ok" {
		v.Users = ptr(len(n.Users))
	}
	return v
}
func (r *Registry) Snapshot(ids []string, now time.Time) (map[string]View, Summary) {
	return r.SnapshotWithQuality(ids, now, nil)
}

// Quality blocks are operator-reviewed source defects, independent of transport
// health. Keep the node in Expected and exclude its identities before unioning.
// A fresh successful RPC (including zero) cannot override a known blind spot.
func (r *Registry) SnapshotWithQuality(ids []string, now time.Time, blocks map[string]string) (map[string]View, Summary) {
	r.mu.Lock()
	defer r.mu.Unlock()
	views := map[string]View{}
	all := map[string]struct{}{}
	summary := Summary{View: View{Status: "unavailable"}}
	for _, id := range ids {
		if _, ok := views[id]; ok {
			continue
		}
		n := r.nodes[id]
		v := view(n, now)
		if reason, blocked := blocks[id]; blocked {
			status := "coverage_unverified"
			if reason == "loopback_untracked" {
				status = reason
			}
			v = View{Status: status}
		}
		views[id] = v
		summary.Expected++
		if v.Users != nil {
			summary.Ready++
			for u := range n.Users {
				all[u] = struct{}{}
			}
		}
	}
	if summary.Ready > 0 {
		summary.ObservedUsers = ptr(len(all))
		summary.Status = "partial"
	}
	if summary.Ready == summary.Expected && summary.Expected > 0 {
		summary.Users = ptr(len(all))
		summary.Status = "ok"
	}
	// Forgotten monitor nodes must not retain identities indefinitely.
	for id, n := range r.nodes {
		if _, ok := views[id]; !ok || now.Sub(n.Seen) > 5*time.Minute {
			delete(r.nodes, id)
		}
	}
	return views, summary
}

type Sender struct {
	Epoch    string
	seq      uint64
	previous map[string]struct{}
	status   string
	force    bool
	lastFull time.Time
}

func NewSender(epoch string) *Sender { return &Sender{Epoch: epoch, force: true} }
func (s *Sender) Packet(status string, users []string, now time.Time) Packet {
	current := map[string]struct{}{}
	for _, u := range users {
		current[u] = struct{}{}
	}
	s.seq++ // Every attempt is unique, including a full resync after a lost ACK.
	p := Packet{Epoch: s.Epoch, Seq: s.seq, Status: status, Full: s.force || s.status != status || status != "ok" || now.Sub(s.lastFull) > time.Minute}
	if p.Full {
		for u := range current {
			p.Add = append(p.Add, u)
		}
	} else {
		for u := range current {
			if _, ok := s.previous[u]; !ok {
				p.Add = append(p.Add, u)
			}
		}
		for u := range s.previous {
			if _, ok := current[u]; !ok {
				p.Remove = append(p.Remove, u)
			}
		}
	}
	sort.Strings(p.Add)
	sort.Strings(p.Remove)
	return p
}
func (s *Sender) Ack(p Packet, users []string, now time.Time) {
	s.seq = p.Seq
	s.status = p.Status
	s.previous = map[string]struct{}{}
	for _, u := range users {
		s.previous[u] = struct{}{}
	}
	s.force = false
	if p.Full {
		s.lastFull = now
	}
}
func (s *Sender) Resync() { s.force = true }
