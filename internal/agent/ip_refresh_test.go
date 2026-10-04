package agent

import "testing"

func TestRefreshPublicIPChangesAndRetainsLastGoodOnFailure(t *testing.T) {
	a := &Agent{}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "", "not-an-ip", "192.168.1.2"} {
		a.refreshPublicIP(func() string { return ip })
	}
	if got := a.publicIP.Load(); got != "1.1.1.1" {
		t.Fatalf("last valid IP lost: %v", got)
	}
	a.refreshPublicIP(func() string { return "2606:4700:4700::1111" })
	if got := a.publicIP.Load(); got != "2606:4700:4700::1111" {
		t.Fatal("IPv6 change not recorded")
	}
}
