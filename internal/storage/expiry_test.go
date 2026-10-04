package storage

import (
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExpiryBoundaryAliasesAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, _ := Open(path)
	now := time.Now()
	for _, n := range []model.Node{
		{ID: "central", LastContact: now.Add(-24 * time.Hour)},
		{ID: "expired", LastContact: now.Add(-time.Hour - time.Second)},
		{ID: "alias", LastSeen: now},
		{ID: "boundary", LastContact: now.Add(-time.Hour)},
		{ID: "healthy", LastContact: now.Add(-time.Second)},
	} {
		s.UpsertNode(n)
	}
	aliases := map[string]string{"alias": "expired"}
	if err := s.ApplyAliases(aliases); err != nil {
		t.Fatal(err)
	}
	removed, err := s.Expire(now, time.Hour)
	if err != nil || len(removed) != 2 {
		t.Fatalf("removed=%d err=%v", len(removed), err)
	}
	for _, id := range []string{"expired", "alias"} {
		if _, ok := s.GetNode(id); ok {
			t.Fatal("expired identity remains")
		}
	}
	for _, id := range []string{"central", "boundary", "healthy"} {
		if _, ok := s.GetNode(id); !ok {
			t.Fatal("live/boundary node removed")
		}
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApplyAliases(aliases); err != nil {
		t.Fatalf("stale config broke restart: %v", err)
	}
	if len(s.Nodes()) != 3 {
		t.Fatal("expired identities resurrected")
	}
}

func TestReceiptClockAndReconnectPreventExpiry(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "state.json"))
	now := time.Now()
	s.UpsertNode(model.Node{ID: "old-clock", LastSeen: now.Add(-24 * time.Hour)})
	if !s.RecordContact("old-clock", now) {
		t.Fatal("contact rejected")
	}
	if removed, err := s.Expire(now, time.Hour); err != nil || len(removed) != 0 {
		t.Fatal("connected node expired")
	}
	s.UpsertNode(model.Node{ID: "future", LastSeen: now.Add(24 * time.Hour)})
	if _, err := s.Expire(now, time.Hour); err != nil {
		t.Fatal(err)
	}
	removed, err := s.Expire(now.Add(time.Hour+time.Second), time.Hour)
	if err != nil || len(removed) != 2 {
		t.Fatal("future legacy clock prevented expiry")
	}
}

func TestExpiryRollsBackWhenDiskWriteFails(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "state.json"))
	now := time.Now()
	s.UpsertNode(model.Node{ID: "node", LastContact: now.Add(-2 * time.Hour)})
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(s.path+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	removed, err := s.Expire(now, time.Hour)
	if err == nil || len(removed) != 0 {
		t.Fatal("failed write reported deletion")
	}
	if _, ok := s.GetNode("node"); !ok {
		t.Fatal("failed transaction lost node")
	}
	reopened, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.GetNode("node"); !ok {
		t.Fatal("disk data lost")
	}
}

func TestArchivedHeartbeatDoesNotKeepCanonicalAlive(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "state.json"))
	now := time.Now()
	s.UpsertNode(model.Node{ID: "keep", LastContact: now.Add(-2 * time.Hour)})
	s.UpsertNode(model.Node{ID: "old"})
	_ = s.ApplyAliases(map[string]string{"old": "keep"})
	s.RecordContact("old", now)
	removed, err := s.Expire(now, time.Hour)
	if err != nil || len(removed) != 2 {
		t.Fatal("retired stream prevented expiry")
	}
}
