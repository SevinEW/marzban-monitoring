package telegram

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLiveCardRendersAfterQueueWait(t *testing.T) {
	for _, existingID := range []int{0, 42} {
		b := New("TEST_TOKEN", -1)
		var value atomic.Value
		value.Store("old sample")
		started := make(chan struct{}, 1)
		render := func() string {
			text := value.Load().(string)
			select {
			case started <- struct{}{}:
			default:
			}
			return text
		}
		var sentText string
		b.client.Transport = fakeTransport(func(r *http.Request) (*http.Response, error) {
			var payload struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			sentText = payload.Text
			return &http.Response{Status: "200 OK", StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":42}}`))}, nil
		})
		card := LiveCard{MessageID: existingID, LastText: "previous card"}
		b.groupMu.Lock()
		done := make(chan error, 1)
		go func() { done <- b.SyncLiveCardLatest(-1, 2, render, &card, func() error { return nil }) }()
		<-started
		value.Store("latest sample")
		b.groupMu.Unlock()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if sentText != "latest sample" || card.LastText != sentText || card.MessageID != 42 || card.Pending {
			t.Fatalf("sent %q, card %+v", sentText, card)
		}
	}
}

func TestGroupSpacingDoesNotAddResponseDuration(t *testing.T) {
	b := New("TEST_TOKEN", -1)
	var started time.Time
	if err := b.withGroupWrite(-1, func() error {
		started = time.Now()
		time.Sleep(80 * time.Millisecond)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The slot is measured from request start, independent of response latency.
	if d := b.groupNext.Sub(started); d > forumWriteSpacing+20*time.Millisecond || d < forumWriteSpacing-20*time.Millisecond {
		t.Fatalf("slot spacing includes response time: %s", d)
	}
}

func TestExplicitFloodCooldownWins(t *testing.T) {
	b := New("TEST_TOKEN", -1)
	before := time.Now()
	_ = b.withGroupWrite(-1, func() error { return &APIError{Status: "429 Too Many Requests", RetryAfter: 15 * time.Second} })
	if b.groupNext.Before(before.Add(16*time.Second)) || !b.groupNext.Equal(b.groupBlockedUntil) {
		t.Fatalf("cooldown lost: %s", b.groupNext)
	}
}
