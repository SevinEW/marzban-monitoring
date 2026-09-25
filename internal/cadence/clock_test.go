package cadence

import (
	"testing"
	"time"
)

func TestSharedBoundary(t *testing.T) {
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for _, phase := range []time.Duration{0, time.Millisecond, 2 * time.Second, 4999 * time.Millisecond} {
		if got := Next(base.Add(phase)); !got.Equal(base.Add(Interval)) {
			t.Fatalf("phase %s: got %s", phase, got)
		}
	}
	// Missing multiple slots must skip them, not replay or drift from completion.
	if got := Next(base.Add(12700 * time.Millisecond)); !got.Equal(base.Add(15 * time.Second)) {
		t.Fatal(got)
	}
}

func TestClockObservation(t *testing.T) {
	var c Clock
	start := time.Now()
	remote := start.Add(30*time.Second + 50*time.Millisecond)
	c.Observe(remote.Format(time.RFC3339Nano), start, start.Add(100*time.Millisecond))
	if got := time.Duration(c.offset.Load()); got != 30*time.Second {
		t.Fatal(got)
	}
	c.Observe("", start, start)
	c.Observe(start.Format(time.RFC3339Nano), start, start.Add(3*time.Second))
	if got := time.Duration(c.offset.Load()); got != 30*time.Second {
		t.Fatal(got)
	}
}
