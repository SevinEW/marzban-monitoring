package central

import (
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"testing"
)

func TestNaturalForumOrder(t *testing.T) {
	nodes := []model.Node{{Name: "Hetzner10"}, {Name: "Holand3"}, {Name: "Hetzner2"}, {Name: "Hetzner1"}}
	want := []string{"Hetzner1", "Hetzner2", "Hetzner10", "Holand3"}
	for i, n := range sortedForumNodes(nodes) {
		if n.Name != want[i] {
			t.Fatalf("row %d: %s", i, n.Name)
		}
	}
	if nodes[0].Name != "Hetzner10" {
		t.Fatal("mutated input")
	}
}

func TestCPUCapacityDetail(t *testing.T) {
	n := model.Node{Cores: 4, Latest: model.Metric{CPUPercent: 56.2}}
	if got := cpuUsageDetail(n); got != "Used ≈ 2.25 / 4 vCPU" {
		t.Fatal(got)
	}
	n.Cores = 0
	if got := cpuUsageDetail(n); got != "vCPU capacity unavailable" {
		t.Fatal(got)
	}
}
