package agent

import "github.com/SevinEW/marzban-monitoring/internal/model"

// One collector produces samples. A slow sender keeps at most one pending
// sample, replacing it instead of accumulating stale data during an outage.
func offerLatest(ch chan model.Metric, m model.Metric) {
	select {
	case ch <- m:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	ch <- m
}
