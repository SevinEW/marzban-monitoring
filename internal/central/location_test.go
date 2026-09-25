package central

import (
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"testing"
)

func TestCountryGroupingBeforeNaturalName(t *testing.T) {
	nodes := []model.Node{{Name: "Turkey10", Location: model.Location{CountryCode: "TR"}}, {Name: "A", Location: model.Location{CountryCode: "PL"}}, {Name: "Turkey2", Location: model.Location{CountryCode: "TR"}}, {Name: "Unknown"}}
	want := []string{"A", "Turkey2", "Turkey10", "Unknown"}
	for i, n := range sortedForumNodes(nodes) {
		if n.Name != want[i] {
			t.Fatal(i, n.Name)
		}
	}
}
func TestPublicPeerFallback(t *testing.T) {
	if peerPublicIP("1.1.1.1:5678") != "1.1.1.1" || peerPublicIP("127.0.0.1:5") != "" {
		t.Fatal("invalid peer fallback")
	}
}
