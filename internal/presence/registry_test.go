package presence

import (
	"fmt"
	"testing"
	"time"
)

func ident(i int) string { return fmt.Sprintf("%032x", i) }
func packet(seq uint64, full bool, add, remove []string) Packet {
	return Packet{Epoch: ident(999), Seq: seq, Full: full, Status: "ok", Add: add, Remove: remove}
}
func TestDedupeFreshnessAndPrivacy(t *testing.T) {
	r := NewRegistry()
	now := time.Now()
	for id, users := range map[string][]string{"a": {ident(1), ident(2)}, "b": {ident(2), ident(3)}} {
		if e := r.Apply(id, packet(1, true, users, nil), now); e != nil {
			t.Fatal(e)
		}
	}
	nodes, s := r.Snapshot([]string{"a", "b"}, now)
	if s.Users == nil || *s.Users != 3 || *nodes["a"].Users != 2 || s.Devices != nil {
		t.Fatal("incorrect account/device count")
	}
	_, s = r.Snapshot([]string{"a", "b", "missing"}, now)
	if s.Users != nil || *s.ObservedUsers != 3 || s.Ready != 2 {
		t.Fatal("partial coverage claimed as total")
	}
	_, s = r.Snapshot([]string{"a", "b"}, now.Add(FreshFor+time.Millisecond))
	if s.Users != nil || s.ObservedUsers != nil {
		t.Fatal("stale data visible")
	}
}
func TestSequenceAndRestart(t *testing.T) {
	r := NewRegistry()
	now := time.Now()
	p := packet(1, true, []string{ident(1)}, nil)
	if e := r.Apply("a", p, now); e != nil {
		t.Fatal(e)
	}
	if e := r.Apply("a", packet(3, false, []string{ident(2)}, nil), now); e != ErrResync {
		t.Fatal("accepted gap")
	}
	if e := r.Apply("a", packet(2, false, []string{ident(2)}, []string{ident(1)}), now); e != nil {
		t.Fatal(e)
	}
	if e := r.Apply("a", p, now); e != ErrResync {
		t.Fatal("accepted replay")
	}
	p.Epoch = ident(888)
	p.Add = nil
	if e := r.Apply("a", p, now); e != nil {
		t.Fatal(e)
	}
	nodes, _ := r.Snapshot([]string{"a"}, now)
	if nodes["a"].Users == nil || *nodes["a"].Users != 0 {
		t.Fatal("true zero unavailable")
	}
	old := packet(7, true, []string{ident(1)}, nil)
	if e := r.Apply("a", old, now); e != ErrResync {
		t.Fatal("accepted retired epoch")
	}
}
func TestReplayDoesNotRefreshAndFailureClears(t *testing.T) {
	r := NewRegistry()
	now := time.Now()
	p := packet(1, true, []string{ident(1)}, nil)
	r.Apply("a", p, now)
	r.Apply("a", p, now.Add(5*time.Second))
	v, _ := r.Snapshot([]string{"a"}, now.Add(7*time.Second))
	if v["a"].Users != nil {
		t.Fatal("replay refreshed sample")
	}
	p = packet(2, true, nil, nil)
	p.Status = "source_changed"
	r.Apply("a", p, now)
	v, _ = r.Snapshot([]string{"a"}, now)
	if v["a"].Users != nil {
		t.Fatal("failed source shown as zero")
	}
}
func TestMalformedAtomicAndSenderResync(t *testing.T) {
	r := NewRegistry()
	now := time.Now()
	s := NewSender(ident(999))
	users := []string{ident(1)}
	p := s.Packet("ok", users, now)
	if !p.Full {
		t.Fatal("initial delta")
	}
	r.Apply("a", p, now)
	s.Ack(p, users, now)
	bad := packet(2, false, []string{ident(2), "bad"}, nil)
	if r.Apply("a", bad, now) == nil {
		t.Fatal("invalid identity accepted")
	}
	v, _ := r.Snapshot([]string{"a"}, now)
	if *v["a"].Users != 1 {
		t.Fatal("partial application")
	}
	p = s.Packet("ok", users, now)
	if p.Full || len(p.Add) > 0 || len(p.Remove) > 0 {
		t.Fatal("heartbeat repeats full set")
	}
	s.Resync()
	if !s.Packet("ok", users, now).Full {
		t.Fatal("resync missing")
	}
}

func TestLostAcknowledgmentResyncUsesNewSequence(t *testing.T) {
	r := NewRegistry()
	s := NewSender(ident(999))
	now := time.Now()
	first := s.Packet("ok", []string{ident(1)}, now)
	if err := r.Apply("a", first, now); err != nil {
		t.Fatal(err)
	}
	// Response lost: sender cannot know whether Central applied the packet.
	s.Resync()
	next := s.Packet("ok", []string{ident(2)}, now)
	if next.Seq <= first.Seq || !next.Full {
		t.Fatal("resync reused an uncertain sequence")
	}
	if err := r.Apply("a", next, now); err != nil {
		t.Fatal(err)
	}
	if _, exists := r.nodes["a"].Users[ident(2)]; !exists {
		t.Fatal("new state ignored after lost acknowledgment")
	}
	s.Ack(next, []string{ident(2)}, now)
	delta := s.Packet("ok", nil, now)
	if err := r.Apply("a", delta, now); err != nil {
		t.Fatal(err)
	}
	if len(r.nodes["a"].Users) != 0 {
		t.Fatal("resync left phantom users")
	}
}
func BenchmarkFleet8000(b *testing.B) {
	r := NewRegistry()
	now := time.Now()
	ids := []string{}
	for n := 0; n < 24; n++ {
		id := fmt.Sprint(n)
		ids = append(ids, id)
		users := []string{}
		for i := 0; i < 400; i++ {
			users = append(users, ident((n*350+i)%8000))
		}
		r.Apply(id, packet(1, true, users, nil), now)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Snapshot(ids, now)
	}
}
