package agent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func machineFingerprint(machine, product string) string {
	machine = strings.ToLower(strings.TrimSpace(machine))
	product = strings.ToLower(strings.TrimSpace(product))
	// Empty/template IDs are not an identity. A per-installation random key is
	// safer than combining unrelated machines that share a template or hostname.
	if len(machine) != 32 || strings.Trim(machine, "0") == "" {
		return ""
	}
	if _, err := hex.DecodeString(machine); err != nil {
		return ""
	}
	if product == "" || strings.Trim(strings.ReplaceAll(product, "-", ""), "0f") == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("marzwatch-machine-v1\x00" + machine + "\x00" + product))
	return hex.EncodeToString(sum[:])
}

func loadMachineKey() (string, error) {
	// Prefer the saved key even if access to DMI changes across an update.
	path := filepath.Join(filepath.Dir(identityPath), "machine-key")
	if b, err := os.ReadFile(path); err == nil {
		key := strings.TrimSpace(string(b))
		if len(key) != 64 {
			return "", fmt.Errorf("invalid saved machine key")
		}
		if _, err := hex.DecodeString(key); err != nil {
			return "", fmt.Errorf("invalid saved machine key")
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	m, _ := os.ReadFile("/etc/machine-id")
	p, _ := os.ReadFile("/sys/class/dmi/id/product_uuid")
	key := machineFingerprint(string(m), string(p))
	if key == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		key = hex.EncodeToString(b)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return "", err
	}
	if err := os.WriteFile(path+".tmp", []byte(key+"\n"), 0600); err != nil {
		return "", err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return "", err
	}
	return key, nil
}
