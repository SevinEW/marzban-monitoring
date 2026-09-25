package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestAllEmojiIDsAndUTF16Offsets(t *testing.T) {
	if len(monitoringEmoji) != 36 {
		t.Fatal("incomplete mapping")
	}
	for _, item := range monitoringEmoji {
		source := "┃ سرور 😎 " + item.Original + " 2/4 ▰▱\n"
		rendered, entities := emojiText(source)
		if len(entities) != 1 || entities[0].CustomEmojiID != item.ID {
			t.Fatalf("missing emoji %s", item.Original)
		}
		units := utf16.Encode([]rune(rendered))
		e := entities[0]
		if string(utf16.Decode(units[e.Offset:e.Offset+e.Length])) != item.Alternative {
			t.Fatalf("wrong offset for %s", item.Original)
		}
		if !strings.HasSuffix(rendered, " 2/4 ▰▱\n") {
			t.Fatal("layout changed")
		}
		if strings.Contains(rendered, "tg://") {
			t.Fatal("raw link leaked")
		}
	}
	rendered, entities := emojiText("💠🛰📍⚡🇹🇷")
	if rendered != "#️⃣📍📍🔲🇹🇷" || len(entities) != 5 {
		t.Fatal("adjacent mapping changed order")
	}
}

func TestCustomEmojiOnSendAndEditButNotTopicName(t *testing.T) {
	payload := map[string]any{"chat_id": 1, "text": "💠 Overview"}
	for _, method := range []string{"sendMessage", "editMessageText"} {
		got := customEmojiPayload(method, payload)
		if _, ok := got["entities"]; !ok {
			t.Fatal("entities missing")
		}
	}
	if payload["text"] != "💠 Overview" {
		t.Fatal("input mutated")
	}
	got := customEmojiPayload("editForumTopic", payload)
	if _, ok := got["entities"]; ok {
		t.Fatal("unsupported topic name markup")
	}
}

func TestEmojiPermissionFallbackDoesNotResendImmediately(t *testing.T) {
	b := New("TEST", 1)
	calls := 0
	b.client.Transport = fakeTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		if calls == 1 {
			if p["entities"] == nil {
				t.Fatal("custom entities missing")
			}
			return &http.Response{Status: "400 Bad Request", StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"ok":false,"description":"CUSTOM_EMOJI_INVALID"}`))}, nil
		}
		if p["entities"] != nil {
			t.Fatal("plain fallback not applied")
		}
		return &http.Response{Status: "200 OK", StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":true}`))}, nil
	})
	p := map[string]any{"chat_id": 1, "text": "💠"}
	if err := b.call("editMessageText", p, nil); err == nil || calls != 1 {
		t.Fatal("unsafe immediate retry")
	}
	if err := b.call("editMessageText", p, nil); err != nil {
		t.Fatal(err)
	}
}

func TestEmojiTransportErrorNeverTriggersFallback(t *testing.T) {
	b := New("TEST", 1)
	calls := 0
	b.client.Transport = fakeTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("timeout") })
	_ = b.call("sendMessage", map[string]any{"chat_id": 1, "text": "💠"}, nil)
	if calls != 1 || b.plainEmoji.Load() {
		t.Fatal("ambiguous send retried or altered")
	}
}
