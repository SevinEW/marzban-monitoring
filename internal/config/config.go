package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultPath = "/etc/marzwatch/config.json"

type Config struct {
	Role            string `json:"role"`
	Name            string `json:"name"`
	Listen          string `json:"listen,omitempty"`
	PublicIP        string `json:"public_ip,omitempty"`
	Timezone        string `json:"timezone,omitempty"`
	TelegramToken   string `json:"telegram_token,omitempty"`
	AdminChatID     int64  `json:"admin_chat_id,omitempty"`
	JoinToken       string `json:"join_token,omitempty"`
	CentralURL      string `json:"central_url,omitempty"`
	CertFingerprint string `json:"cert_fingerprint,omitempty"`
}

func Load(path string) (Config, error) {
	if path == "" {
		path = DefaultPath
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	if c.Role != "central" && c.Role != "agent" {
		return Config{}, errors.New("invalid role")
	}
	return c, nil
}

func Save(path string, c Config) error {
	if path == "" {
		path = DefaultPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func RandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func ParseJoinKey(k string) (token, fingerprint string, err error) {
	parts := strings.Split(strings.TrimSpace(k), ".")
	if len(parts) != 3 || parts[0] != "MW1" {
		return "", "", fmt.Errorf("invalid join key")
	}
	if len(parts[1]) < 32 || len(parts[2]) != 64 {
		return "", "", fmt.Errorf("invalid join key")
	}
	return parts[1], strings.ToLower(parts[2]), nil
}
