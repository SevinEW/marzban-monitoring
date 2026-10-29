package storage

import (
	"math/bits"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/reportclock"
)

// addDailyTraffic splits a counter interval at Iran calendar midnights. The
// collector has no per-packet timestamps, so boundary attribution is proportional
// to elapsed time. Integer cumulative shares preserve every byte without float
// rounding, even for counters larger than 2^53. Caller holds the Store mutex.
func addDailyTraffic(n *model.Node, prev, next model.Metric) {
	if prev.Timestamp.IsZero() || !next.Timestamp.After(prev.Timestamp) {
		return
	}
	var rx, tx uint64
 if next.NetworkSource != "" || prev.NetworkSource != "" {
  if next.NetworkSource != model.NetworkSource || prev.NetworkSource != next.NetworkSource || next.NetworkBoot == "" || next.NetworkBoot != prev.NetworkBoot { return }
  var valid bool
  rx,tx,valid=model.NetworkDelta(prev.NetworkCounters,next.NetworkCounters)
  if !valid { return }
 } else {
	// A decreasing counter means reboot/reset; never subtract unsigned counters.
	if next.RXBytesTotal >= prev.RXBytesTotal {
		rx = next.RXBytesTotal - prev.RXBytesTotal
	}
	if next.TXBytesTotal >= prev.TXBytesTotal {
		tx = next.TXBytesTotal - prev.TXBytesTotal
	}
 }
	if rx == 0 && tx == 0 {
		return
	}
	total := uint64(next.Timestamp.Sub(prev.Timestamp))
	share := func(v uint64, t time.Time) uint64 {
		elapsed := uint64(t.Sub(prev.Timestamp))
		hi, lo := bits.Mul64(v, elapsed)
		q, _ := bits.Div64(hi, lo, total)
		return q
	}
	// Bound work to the same fourteen calendar dates retained by the store.
	day := reportclock.Midnight(next.Timestamp).AddDate(0, 0, -13)
	if start := reportclock.Midnight(prev.Timestamp); start.After(day) {
		day = start
	}
	for day.Before(next.Timestamp) {
		end := day.AddDate(0, 0, 1)
		a, b := day, end
		if a.Before(prev.Timestamp) {
			a = prev.Timestamp
		}
		if b.After(next.Timestamp) {
			b = next.Timestamp
		}
		if b.After(a) {
			date := reportclock.Date(day)
			d, ok := n.Daily[date]
			if !ok {
				d = model.NewDaily(date)
			}
			d.RXBytes += share(rx, b) - share(rx, a)
			d.TXBytes += share(tx, b) - share(tx, a)
			n.Daily[date] = d
		}
		day = end
	}
}
