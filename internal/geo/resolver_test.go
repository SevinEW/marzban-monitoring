package geo

import (
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"testing"
)

func TestCountryConflictDoesNotGuess(t *testing.T) {
	a := model.Location{CountryCode: "TR", City: "Istanbul"}
	b := model.Location{CountryCode: "GB", City: "London"}
	got := consensus("1.1.1.1", a, b)
	if got.CountryCode != "" || got.City != "" || got.PublicIP != "1.1.1.1" {
		t.Fatal(got)
	}
	b.CountryCode = "TR"
	if got = consensus("1.1.1.1", a, b); got.CountryCode != "TR" || got.City != "" {
		t.Fatal(got)
	}
}
func TestPublicIPRejectsInvalidAndLocal(t *testing.T) {
	for _, s := range []string{"", "Unknown", "<html>", "127.0.0.1", "10.0.0.1", "::1"} {
		if PublicIP(s) != "" {
			t.Fatal(s)
		}
	}
}
