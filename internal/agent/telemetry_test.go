package agent

import (
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"testing"
	"time"
)

func TestOutageKeepsOnlyLatestSample(t *testing.T) {
	ch := make(chan model.Metric, 1)
	for i := int64(1); i <= 10000; i++ {
		offerLatest(ch, model.Metric{Timestamp: time.Unix(i, 0)})
	}
	if len(ch) != 1 || (<-ch).Timestamp.Unix() != 10000 {
		t.Fatal("stale samples accumulated")
	}
	offerLatest(ch, model.Metric{Timestamp: time.Unix(10001, 0)})
	if (<-ch).Timestamp.Unix() != 10001 {
		t.Fatal("sender did not recover")
	}
}
