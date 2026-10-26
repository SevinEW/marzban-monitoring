package agent

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/SevinEW/marzban-monitoring/internal/config"
	"github.com/SevinEW/marzban-monitoring/internal/security"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// FetchRelease uses the same certificate pin and per-node identity as metrics.
// It stages a whole checksum-verified bundle; nothing is executed or installed.
func FetchRelease(destination string) error {
	cfg, e := config.Load("")
	if e != nil {
		return e
	}
	if cfg.Role != "agent" {
		return errors.New("agent required")
	}
	raw, e := os.ReadFile(identityPath)
	if e != nil {
		return e
	}
	var id identity
	if json.Unmarshal(raw, &id) != nil || id.NodeID == "" || id.NodeSecret == "" {
		return errors.New("missing identity")
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true, VerifyPeerCertificate: security.VerifyFingerprint(strings.ToLower(cfg.CertFingerprint))}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect denied") }}
	return fetchReleaseBundle(destination, runtime.GOARCH, func(name string) ([]byte, error) {
		body := []byte(name)
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(id.NodeSecret))
		mac.Write([]byte("release-v1\n" + ts + "\n"))
		mac.Write(body)
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		req, e := http.NewRequestWithContext(ctx, "POST", cfg.CentralURL+"/api/v1/release", bytes.NewReader(body))
		if e != nil {
			return nil, e
		}
		req.Header.Set("X-MW-Node", id.NodeID)
		req.Header.Set("X-MW-Time", ts)
		req.Header.Set("X-MW-Signature", hex.EncodeToString(mac.Sum(nil)))
		resp, e := client.Do(req)
		if e != nil {
			return nil, errors.New("central download unavailable")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("central download status %d", resp.StatusCode)
		}
		limit := int64(32 << 20)
		if name == "SHA256SUMS" {
			limit = 4096
		}
		data, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if e != nil || int64(len(data)) > limit {
			return nil, errors.New("invalid release response")
		}
		return data, nil
	})
}

func fetchReleaseBundle(destination, arch string, get func(string) ([]byte, error)) error {
	if arch != "amd64" && arch != "arm64" {
		return errors.New("unsupported architecture")
	}
	manifest, e := get("SHA256SUMS")
	if e != nil {
		return e
	}
	sums := map[string]string{}
	for _, line := range strings.Split(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return errors.New("invalid manifest")
		}
		sha, e := hex.DecodeString(fields[0])
		if e != nil || len(sha) != 32 || sums[fields[1]] != "" {
			return errors.New("invalid checksum")
		}
		sums[fields[1]] = strings.ToLower(fields[0])
	}
	stage, e := os.MkdirTemp(destination, ".central-release-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	names := []string{"marzwatch-linux-" + arch, "marzwatch-auto-update.sh"}
	for _, name := range names {
		if sums[name] == "" {
			return errors.New("missing checksum")
		}
		data, e := get(name)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != sums[name] {
			return errors.New("checksum mismatch")
		}
		if e = os.WriteFile(filepath.Join(stage, name), data, 0600); e != nil {
			return e
		}
	}
	if e = os.WriteFile(filepath.Join(stage, "SHA256SUMS"), manifest, 0600); e != nil {
		return e
	}
	for _, name := range append(names, "SHA256SUMS") {
		if e = os.Rename(filepath.Join(stage, name), filepath.Join(destination, name)); e != nil {
			return e
		}
	}
	return nil
}
