package storage

import (
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestLocationRepairPreservesMetricsAndIdentity(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now()
	s.UpsertNode(model.Node{ID: "n", Secret: "test", LastSeen: stamp, Latest: model.Metric{CPUPercent: 42}, Location: model.Location{CountryCode: "GB"}})
	s.FillPublicIP("n", "1.1.1.1")
	s.SetLocation("n", model.Location{CountryCode: "TR", Country: "Turkey"})
	n, _ := s.GetNode("n")
	if n.Secret != "test" || n.Latest.CPUPercent != 42 || !n.LastSeen.Equal(stamp) || n.Location.PublicIP != "1.1.1.1" || n.Location.CountryCode != "TR" {
		t.Fatal("metadata repair changed metric/identity or lost address")
	}
	s.SetPublicIP("n", "8.8.8.8")
	n, _ = s.GetNode("n")
	if n.Location.CountryCode != "" || n.Location.PublicIP != "8.8.8.8" || n.Latest.CPUPercent != 42 {
		t.Fatal("old-IP location survived new IP")
	}
}
