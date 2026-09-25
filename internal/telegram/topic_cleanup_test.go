package telegram

import (
	"encoding/json"
	"testing"
)

func TestCleanupOnlyOwnKnownTopicRename(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{`{"message":{"message_id":10,"message_thread_id":4,"from":{"id":2},"chat":{"id":-1},"forum_topic_edited":{"name":"TR Turkey2"}}}`, true},
		{`{"message":{"message_id":10,"message_thread_id":4,"from":{"id":3},"chat":{"id":-1},"forum_topic_edited":{"name":"TR Turkey2"}}}`, false},
		{`{"message":{"message_id":10,"message_thread_id":5,"from":{"id":2},"chat":{"id":-1},"forum_topic_edited":{"name":"TR Turkey2"}}}`, false},
		{`{"message":{"message_id":10,"message_thread_id":4,"from":{"id":2},"chat":{"id":-1},"text":"monitor card"}}`, false},
	} {
		var e topicEvent
		if err := json.Unmarshal([]byte(tc.body), &e); err != nil {
			t.Fatal(err)
		}
		if got := e.ownRename(-1, 2, func(id int) bool { return id == 4 }); got != tc.want {
			t.Fatal(tc.body)
		}
	}
}
