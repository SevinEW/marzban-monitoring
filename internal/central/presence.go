package central

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/presence"
)

func (s *Server) receivePresence(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("X-MW-Node")
	node, ok := s.store.GetNode(id)
	if !ok || id == "central" {
		http.Error(w, "unauthorized", 401)
		return
	}
	stamp := r.Header.Get("X-MW-Time")
	ts, e := strconv.ParseInt(stamp, 10, 64)
	if e != nil || math.Abs(float64(time.Now().Unix())-float64(ts)) > 30 {
		http.Error(w, "stale request", 401)
		return
	}
	body, e := io.ReadAll(io.LimitReader(r.Body, presence.MaxBody+1))
	if e != nil || len(body) > presence.MaxBody {
		http.Error(w, "invalid request", 400)
		return
	}
	mac := hmac.New(sha256.New, []byte(node.Secret))
	mac.Write([]byte("presence-v1\n" + stamp + "\n"))
	mac.Write(body)
	signature, e := hex.DecodeString(r.Header.Get("X-MW-Signature"))
	if e != nil || !hmac.Equal(mac.Sum(nil), signature) {
		http.Error(w, "unauthorized", 401)
		return
	}
	var packet presence.Packet
	if json.Unmarshal(body, &packet) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	// Archived aliases retain credentials but cannot overwrite the surviving source.
	if !s.store.IsActiveNode(id) {
		http.Error(w, "retired source", 409)
		return
	}

	if e = s.presence.Apply(id, packet, time.Now()); e != nil {
		http.Error(w, "resync required", 409)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) localPresence() {
	c := presence.NewCollector(presence.ConfigPath)
	defer c.Close()
	var epoch [16]byte
	if _, e := rand.Read(epoch[:]); e != nil {
		return
	}
	sender := presence.NewSender(hex.EncodeToString(epoch[:]))
	generation := ""
	for {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		sample := c.Sample(ctx)
		cancel()
		if generation != sample.Generation {
			sender.Resync()
			generation = sample.Generation
		}
		p := sender.Packet(sample.Status, sample.Users, time.Now())
		if s.presence.Apply("central", p, time.Now()) == nil {
			sender.Ack(p, sample.Users, time.Now())
		} else {
			sender.Resync()
		}
		wait := time.Second
		if sample.Status != "ok" {
			wait = 5 * time.Second
		}
		timer := time.NewTimer(max(100*time.Millisecond, wait-time.Since(start)))
		select {
		case <-timer.C:
		case <-s.stop:
			timer.Stop()
			return
		}
	}
}
