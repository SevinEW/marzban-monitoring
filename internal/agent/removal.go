package agent

import (
	"os"
	"regexp"
)

var removalNonce = regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)

// This is a wake-up hint, not authority. The fixed root helper independently
// authenticates a fresh plan and acceptance over the pinned Central channel.
func (a *Agent) handleRemoval(nonce string) {
	if !removalNonce.MatchString(nonce) {
		return
	}
	const path = "/var/lib/marzwatch/remove.request"
	old, _ := os.ReadFile(path)
	if string(old) == nonce {
		return
	}
	if os.WriteFile(path+".new", []byte(nonce), 0600) == nil {
		_ = os.Rename(path+".new", path)
	}
}
