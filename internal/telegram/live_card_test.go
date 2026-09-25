package telegram

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type fakeTransport func(*http.Request) (*http.Response, error)

func (f fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLiveCardRecovery(t *testing.T) {
	tests := []struct {
		name      string
		responses []string
		wantCalls []string
		wantID    int
		wantError bool
	}{
		{"edit", []string{`{"ok":true,"result":true}`}, []string{"editMessageText"}, 10, false},
		{"not modified", []string{`{"ok":false,"description":"Bad Request: message is not modified"}`}, []string{"editMessageText"}, 10, false},
		{"network", []string{"transport"}, []string{"editMessageText"}, 10, true},
		{"flood", []string{`{"ok":false,"description":"Too Many Requests","parameters":{"retry_after":30}}`}, []string{"editMessageText"}, 10, true},
		{"missing", []string{`{"ok":false,"description":"Bad Request: message to edit not found"}`, `{"ok":true,"result":{"message_id":20}}`}, []string{"editMessageText", "sendMessage"}, 20, false},
		{"uneditable", []string{`{"ok":false,"description":"Bad Request: message can't be edited"}`, `{"ok":true,"result":true}`, `{"ok":true,"result":{"message_id":20}}`}, []string{"editMessageText", "deleteMessage", "sendMessage"}, 20, false},
		{"delete fails", []string{`{"ok":false,"description":"Bad Request: message can't be edited"}`, "transport"}, []string{"editMessageText", "deleteMessage"}, 10, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			b := New("TEST_TOKEN", 1)
			b.client.Transport = fakeTransport(func(r *http.Request) (*http.Response, error) {
				method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				calls = append(calls, method)
				if len(calls) > len(tt.responses) {
					t.Fatalf("unexpected call %s", method)
				}
				body := tt.responses[len(calls)-1]
				if body == "transport" {
					return nil, errors.New("connection lost")
				}
				status, code := "200 OK", 200
				if strings.Contains(body, `"ok":false`) {
					status, code = "400 Bad Request", 400
				}
				return &http.Response{Status: status, StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			card := LiveCard{MessageID: 10, LastText: "old"}
			err := b.SyncLiveCard(1, 2, "new", &card, func() error { return nil })
			if (err != nil) != tt.wantError || card.MessageID != tt.wantID || !reflect.DeepEqual(calls, tt.wantCalls) {
				t.Fatalf("card=%+v calls=%v err=%v", card, calls, err)
			}
			if err != nil && strings.Contains(err.Error(), "TEST_TOKEN") {
				t.Fatal("token leaked")
			}
		})
	}
}

func TestAmbiguousSendNotRetriedAfterRestart(t *testing.T) {
	b := New("TEST_TOKEN", 1)
	calls := 0
	b.client.Transport = fakeTransport(func(r *http.Request) (*http.Response, error) { calls++; return nil, errors.New("timeout") })
	card := LiveCard{}
	var persisted LiveCard
	save := func() error { persisted = card; return nil }
	if b.SyncLiveCard(1, 2, "new", &card, save) == nil {
		t.Fatal("expected error")
	}
	card = persisted
	if b.SyncLiveCard(1, 2, "new", &card, save) == nil {
		t.Fatal("expected pending error")
	}
	if calls != 1 || !persisted.Pending {
		t.Fatalf("calls=%d persisted=%+v", calls, persisted)
	}
}

func TestPersistFailurePreventsSend(t *testing.T) {
	b := New("TEST_TOKEN", 1)
	b.client.Transport = fakeTransport(func(r *http.Request) (*http.Response, error) { t.Fatal("unexpected send"); return nil, nil })
	card := LiveCard{}
	if b.SyncLiveCard(1, 2, "new", &card, func() error { return errors.New("disk full") }) == nil {
		t.Fatal("expected error")
	}
}
