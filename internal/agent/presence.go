package agent

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/presence"
)

func presenceEpoch() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
func (a *Agent) presenceLoop() {
	collector := presence.NewCollector(presence.ConfigPath)
	defer collector.Close()
	sender := presence.NewSender(presenceEpoch())
	generation := ""
	delay := time.Second
	for {
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		sample := collector.Sample(ctx)
		cancel()
		if sample.Generation != generation {
			sender = presence.NewSender(presenceEpoch())
			generation = sample.Generation
		}
		p := sender.Packet(sample.Status, sample.Users, time.Now())
		b, e := json.Marshal(p)
		if e == nil && len(b) <= presence.MaxBody {
			// Registration may replace identity concurrently; its atomic file is the source.
			raw, err := os.ReadFile(identityPath)
			var id identity
			if err == nil && json.Unmarshal(raw, &id) == nil && id.NodeID != "" && id.NodeSecret != "" {
				ts := strconv.FormatInt(a.clock.Now().Unix(), 10)
				mac := hmac.New(sha256.New, []byte(id.NodeSecret))
				mac.Write([]byte("presence-v1\n" + ts + "\n"))
				mac.Write(b)
				send, done := context.WithTimeout(context.Background(), 2*time.Second)
				req, err := http.NewRequestWithContext(send, http.MethodPost, a.cfg.CentralURL+"/api/v1/presence", bytes.NewReader(b))
				if err == nil {
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("X-MW-Node", id.NodeID)
					req.Header.Set("X-MW-Time", ts)
					req.Header.Set("X-MW-Signature", hex.EncodeToString(mac.Sum(nil)))
					resp, err := a.client.Do(req)
					if err == nil {
						io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
						resp.Body.Close()
						if resp.StatusCode == 204 {
							sender.Ack(p, sample.Users, time.Now())
							delay = time.Second
						} else {
							sender.Resync()
							delay = min(delay*2, 30*time.Second)
						}
					} else {
						sender.Resync()
						delay = min(delay*2, 30*time.Second)
					}
				}
				done()
			}
		}
		if sample.Status != "ok" && delay < 5*time.Second {
			delay = 5 * time.Second
		}
		// No overlap, no queue, and bounded retry traffic when Central is unavailable.
		time.Sleep(max(100*time.Millisecond, delay-time.Since(started)))
	}
}
