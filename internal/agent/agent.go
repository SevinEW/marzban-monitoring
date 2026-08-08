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
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/collector"
	"github.com/SevinEW/marzban-monitoring/internal/config"
	"github.com/SevinEW/marzban-monitoring/internal/geo"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/security"
)

const identityPath = "/var/lib/marzwatch/identity.json"

var errIdentityRejected = errors.New("central rejected node identity")

type identity struct {
	NodeID     string `json:"node_id"`
	NodeSecret string `json:"node_secret"`
}

type Agent struct {
	cfg    config.Config
	id     identity
	client *http.Client
	col    *collector.Collector
	mu     sync.RWMutex
	latest model.Metric
}

func Run(cfg config.Config) error {
	tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true, VerifyPeerCertificate: security.VerifyFingerprint(strings.ToLower(cfg.CertFingerprint))}, MaxIdleConns: 4, MaxIdleConnsPerHost: 2, IdleConnTimeout: 60 * time.Second}
	a := &Agent{cfg: cfg, client: &http.Client{Transport: tr, Timeout: 12 * time.Second}, col: collector.New()}
	_ = a.loadIdentity()
	if a.id.NodeID == "" {
		if err := a.registerWithRetry(); err != nil {
			return err
		}
	}
	go a.superviseCollector()
	return a.sendLoop()
}

func (a *Agent) registerWithRetry() error {
	delay := 2 * time.Second
	for {
		if err := a.register(); err == nil {
			return nil
		} else {
			log.Printf("registration failed: %v", err)
		}
		time.Sleep(delay)
		if delay < time.Minute {
			delay *= 2
			if delay > time.Minute {
				delay = time.Minute
			}
		}
	}
}

func (a *Agent) register() error {
	host, osName, arch, cores := collector.SystemInfo()
	loc := geo.Detect()
	q := model.RegisterRequest{Name: a.cfg.Name, Hostname: host, OS: osName, Arch: arch, Cores: cores, Location: loc}
	b, _ := json.Marshal(q)
	req, _ := http.NewRequest(http.MethodPost, a.cfg.CentralURL+"/api/v1/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Marzwatch-Join", a.cfg.JoinToken)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("central %s: %s", resp.Status, string(msg))
	}
	var rr model.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return err
	}
	if rr.NodeID == "" || rr.NodeSecret == "" {
		return fmt.Errorf("invalid registration response")
	}
	a.id = identity{NodeID: rr.NodeID, NodeSecret: rr.NodeSecret}
	return a.saveIdentity()
}

func (a *Agent) superviseCollector() {
	for {
		func() {
			defer func() {
				if x := recover(); x != nil {
					log.Printf("collector panic recovered: %v", x)
				}
			}()
			a.collectLoop()
		}()
		time.Sleep(5 * time.Second)
	}
}

func (a *Agent) collectLoop() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for range t.C {
		m, err := a.col.Collect()
		if err != nil {
			log.Printf("collect partial error: %v", err)
		}
		a.mu.Lock()
		a.latest = m
		a.mu.Unlock()
	}
}

func (a *Agent) sendLoop() error {
	time.Sleep(17 * time.Second)
	delay := time.Minute
	for {
		a.mu.RLock()
		m := a.latest
		a.mu.RUnlock()
		if m.Timestamp.IsZero() {
			m, _ = a.col.Collect()
		}
		if err := a.sendMetric(m); err != nil {
			if errors.Is(err, errIdentityRejected) {
				log.Printf("node identity rejected; starting safe automatic re-registration")
				if err := a.resetIdentity(); err != nil {
					log.Printf("identity reset failed: %v", err)
				} else if err := a.registerWithRetry(); err != nil {
					log.Printf("automatic re-registration failed: %v", err)
				} else {
					log.Printf("automatic re-registration completed")
					delay = time.Minute
					continue
				}
			}
			log.Printf("metrics send failed: %v", err)
			if delay < 5*time.Minute {
				delay *= 2
				if delay > 5*time.Minute {
					delay = 5 * time.Minute
				}
			}
		} else {
			delay = time.Minute
		}
		time.Sleep(delay)
	}
}

func (a *Agent) sendMetric(m model.Metric) error {
	b, _ := json.Marshal(m)
	ts := fmt.Sprintf("%d", time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(a.id.NodeSecret))
	mac.Write([]byte(ts))
	mac.Write([]byte("\n"))
	mac.Write(b)
	sig := hex.EncodeToString(mac.Sum(nil))
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.CentralURL+"/api/v1/metrics", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MW-Node", a.id.NodeID)
	req.Header.Set("X-MW-Time", ts)
	req.Header.Set("X-MW-Signature", sig)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errIdentityRejected
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("central returned %s", resp.Status)
	}
	return nil
}

func (a *Agent) resetIdentity() error {
	if _, err := os.Stat(identityPath); err == nil {
		stale := fmt.Sprintf("%s.stale-%d", identityPath, time.Now().Unix())
		if err := os.Rename(identityPath, stale); err != nil {
			return err
		}
		_ = os.Chmod(stale, 0600)
	}
	a.id = identity{}
	return nil
}

func (a *Agent) loadIdentity() error {
	b, e := os.ReadFile(identityPath)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, &a.id)
}

func (a *Agent) saveIdentity() error {
	if err := os.MkdirAll(filepath.Dir(identityPath), 0750); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(a.id, "", "  ")
	tmp := identityPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, identityPath)
}
