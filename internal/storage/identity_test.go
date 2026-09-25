package storage

import (
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRegistrationRetriesAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, _ := Open(path)
	key := strings.Repeat("a", 64)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := s.Register(model.Node{ID: "one", Secret: "test", ServerKey: key})
			if err != nil || n.ID != "one" {
				t.Errorf("registration failed: %v", err)
			}
		}()
	}
	wg.Wait()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	n, err := reopened.Register(model.Node{ID: "two", Secret: "different", ServerKey: key})
	if err != nil || n.ID != "one" || n.Secret != "test" || len(reopened.Nodes()) != 1 {
		t.Fatalf("retry forked identity: %v", err)
	}
}

func TestExplicitAliasesRetainHistoryAndCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, _ := Open(path)
	old := model.Node{ID: "old", Secret: "old-secret", Daily: map[string]model.DailyStats{"2026-09-25": {Samples: 42}}}
	s.UpsertNode(old)
	s.UpsertNode(model.Node{ID: "keep", Secret: "new-secret"})
	s.UpsertNode(model.Node{ID: "unrelated"})
	if err := s.ApplyAliases(map[string]string{"old": "keep"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Nodes()) != 2 {
		t.Fatal("archived identity still visible")
	}
	n, ok := s.GetNode("old")
	if !ok || n.Secret != old.Secret || n.Daily["2026-09-25"].Samples != 42 {
		t.Fatal("archive lost")
	}
	n, accepted := s.UpdateMetric("old", model.Metric{Timestamp: time.Now()}, time.UTC)
	if accepted || n.ID != "keep" {
		t.Fatal("archived stream was counted twice")
	}
	if err := s.BindServerKey("keep", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	// The old identity may authenticate but cannot poison the canonical key.
	if err := s.BindServerKey("old", strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	canonical, _ := s.GetNode("keep")
	if canonical.ServerKey != strings.Repeat("a", 64) {
		t.Fatal("archived stream poisoned identity")
	}
}

func TestSameNameOrIPDoesNotMerge(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "state.json"))
	for _, id := range []string{"one", "two"} {
		if _, err := s.Register(model.Node{ID: id, Secret: id, Name: "same", Hostname: "template", Location: model.Location{PublicIP: "192.0.2.1"}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.Nodes()) != 2 {
		t.Fatal("distinct identities were merged")
	}
	if err := s.ApplyAliases(map[string]string{"one": "two", "two": "one"}); err == nil {
		t.Fatal("cycle allowed")
	}
	if err := s.ApplyAliases(map[string]string{"one": "missing"}); err == nil {
		t.Fatal("unknown alias allowed")
	}
}

func TestOldSamplesDoNotRegressOrDoubleCount(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "state.json"))
	s.UpsertNode(model.Node{ID: "one"})
	m := model.Metric{Timestamp: time.Now(), RXBytesTotal: 100}
	s.UpdateMetric("one", m, time.UTC)
	if _, accepted := s.UpdateMetric("one", m, time.UTC); accepted {
		t.Fatal("duplicate accepted")
	}
	m.Timestamp = m.Timestamp.Add(-time.Second)
	if _, accepted := s.UpdateMetric("one", m, time.UTC); accepted {
		t.Fatal("older sample accepted")
	}
}
