package cadence

import (
	"testing"
	"time"
)

func TestSharedBoundary(t *testing.T) {
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for _, phase := range []time.Duration{0, time.Millisecond, Interval / 2, Interval - time.Millisecond} {
		if got := Next(base.Add(phase)); !got.Equal(base.Add(Interval)) {
			t.Fatalf("phase %s: got %s", phase, got)
		}
	}
	// Missing multiple slots must skip them, not replay or drift from completion.
	if got := Next(base.Add(12700 * time.Millisecond)); !got.Equal(base.Add(13 * time.Second)) {
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

func TestOffsetCorrectionCannotRepeatCollectionBoundary(t *testing.T) {
	base := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	// A response arriving during sleep moves the clock back by 5 ms. The old
	// schedule wakes at local second 1, then takes another sample 5 ms later.
	wake := base.Add(time.Second)
	offset := -5 * time.Millisecond
	oldWait := Next(wake.Add(offset)).Sub(wake.Add(offset))
	if oldWait != 5*time.Millisecond {
		t.Fatal(oldWait)
	}
	if got := sampleDelay(wake, wake, offset); got != time.Second+5*time.Millisecond {
		t.Fatal(got)
	}
	for ms := -999; ms <= 999; ms++ {
		d := sampleDelay(wake, wake, time.Duration(ms)*time.Millisecond)
		if d < 3*Interval/4 || d >= 7*Interval/4 {
			t.Fatalf("offset=%d delay=%s", ms, d)
		}
		if wake.Add(time.Duration(ms)*time.Millisecond).Add(d).Nanosecond() != 0 {
			t.Fatal("lost slot alignment")
		}
	}
	if d := sampleDelay(base.Add(1200*time.Millisecond), base, 0); d != 800*time.Millisecond {
		t.Fatal(d)
	}
}
