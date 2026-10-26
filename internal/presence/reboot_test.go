package presence

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestChangedBootRejectsEvenMatchingProcess(t *testing.T) {
	// The wrong boot must be rejected before connecting to the API, even if a
	// process happens to have the same PID/start ticks as the old host boot.
	c := Binding{Schema: 1, BootID: "previous-boot", Namespace: "0123456789abcdef", TrackingVerified: true,
		Cores: []Core{{Address: "127.0.0.1:1", PID: os.Getpid(), StartTicks: "1"}}}
	if start, err := ProcessStart(os.Getpid()); err == nil {
		c.Cores[0].StartTicks = start
	}
	b, _ := json.Marshal(c)
	p := filepath.Join(t.TempDir(), "presence.json")
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	collector := NewCollector(p)
	defer collector.Close()
	s := collector.Sample(context.Background())
	if s.Status != "source_changed" || s.Users != nil || len(collector.conns) != 0 {
		t.Fatal("old boot reached the API or produced a count")
	}
}
