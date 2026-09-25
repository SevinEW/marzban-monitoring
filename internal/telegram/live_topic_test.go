package telegram

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDeletedTopicRecreatedOnce(t *testing.T) {
	b := New("TEST", 1)
	calls := 0
	b.client.Transport = fakeTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"ok":false,"description":"Bad Request: TOPIC_ID_INVALID"}`
		status, code := "400 Bad Request", 400
		if calls == 2 {
			body = `{"ok":true,"result":{"message_thread_id":22}}`
			status, code = "200 OK", 200
		}
		if calls > 2 {
			t.Fatal("extra creation")
		}
		return &http.Response{Status: status, StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	topic := LiveTopic{ID: 11, Name: "node", CheckedAt: time.Now().Add(-6 * time.Minute)}
	var saved LiveTopic
	save := func() error { saved = topic; return nil }
	if err := b.EnsureLiveTopic(1, "node", &topic, save); err != nil {
		t.Fatal(err)
	}
	topic = saved
	if err := b.EnsureLiveTopic(1, "node", &topic, save); err != nil {
		t.Fatal(err)
	}
	if topic.ID != 22 || topic.Pending || calls != 2 {
		t.Fatalf("topic=%+v calls=%d", topic, calls)
	}
}

func TestUnknownTopicCreationNeverRetried(t *testing.T) {
	b := New("TEST", 1)
	calls := 0
	b.client.Transport = fakeTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("timeout") })
	topic := LiveTopic{}
	var saved LiveTopic
	save := func() error { saved = topic; return nil }
	_ = b.EnsureLiveTopic(1, "node", &topic, save)
	topic = saved
	_ = b.EnsureLiveTopic(1, "node", &topic, save)
	if calls != 1 || !topic.Pending {
		t.Fatal("ambiguous request retried")
	}
}

func TestProbeErrorsPreserveTopic(t *testing.T) {
	for _, desc := range []string{"TOPIC_CLOSED", "CHAT_ADMIN_REQUIRED", "Too Many Requests", "Bad Request: chat not found"} {
		t.Run(desc, func(t *testing.T) {
			b := New("TEST", 1)
			calls := 0
			b.client.Transport = fakeTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{Status: "400 Bad Request", StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"ok":false,"description":"` + desc + `"}`))}, nil
			})
			topic := LiveTopic{ID: 11, Name: "node"}
			if err := b.EnsureLiveTopic(1, "node", &topic, func() error { return nil }); err == nil {
				t.Fatal("expected error")
			}
			if topic.ID != 11 || calls != 1 {
				t.Fatal("uncertain topic replaced")
			}
		})
	}
}

func TestTopicIntentMustPersistBeforeCreate(t *testing.T) {
	b := New("TEST", 1)
	b.client.Transport = fakeTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("created without durable intent")
		return nil, nil
	})
	topic := LiveTopic{}
	if err := b.EnsureLiveTopic(1, "node", &topic, func() error { return errors.New("disk full") }); err == nil {
		t.Fatal("expected error")
	}
}
