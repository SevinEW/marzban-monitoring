// Package cadence keeps collection on shared wall-clock boundaries, independent
// of process startup and request duration. It never changes the host clock.
package cadence

import (
	"sync/atomic"
	"time"
)

const Interval = time.Second
const TimeHeader = "X-Marzwatch-Time"

type Clock struct{ offset atomic.Int64 }

func Next(now time.Time) time.Time { return now.Truncate(Interval).Add(Interval) }
func (c *Clock) Now() time.Time    { return time.Now().Add(time.Duration(c.offset.Load())) }
func (c *Clock) Delay() time.Duration {
	now := c.Now()
	return Next(now).Sub(now)
}

// DelayAfter preserves a monotonic minimum interval while still joining shared
// slots. An offset learned while sleeping can move the previous wake just
// before a boundary; joining that boundary again would produce a millisecond
// sample, whose burst rate could replace a full-second sample in the UI.
func (c *Clock) DelayAfter(last time.Time) time.Duration {
	return sampleDelay(time.Now(), last, time.Duration(c.offset.Load()))
}

func sampleDelay(now, last time.Time, offset time.Duration) time.Duration {
	wall := now.Add(offset)
	wait := Next(wall).Sub(wall)
	if !last.IsZero() && now.Sub(last)+wait < 3*Interval/4 {
		wait += Interval
	}
	return wait
}

// Observe uses only short, authenticated exchanges. Slow responses give an
// unreliable clock estimate; absent headers keep older Centrals compatible.
func (c *Clock) Observe(header string, started, received time.Time) {
	remote, err := time.Parse(time.RFC3339Nano, header)
	rtt := received.Sub(started)
	if err != nil || rtt < 0 || rtt > 2*time.Second {
		return
	}
	midpoint := started.Add(rtt / 2)
	c.offset.Store(int64(remote.Sub(midpoint)))
}
