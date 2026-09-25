package telegram

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type LiveTopic struct {
	ID        int
	Name      string
	Pending   bool
	CheckedAt time.Time
}

func IsTopicMissing(err error) bool {
	d := apiDescription(err)
	return strings.Contains(d, "topic_id_invalid") || strings.Contains(d, "message thread not found") || strings.Contains(d, "topic_deleted")
}

// Existing topics are probed even when an offline card's text never changes.
// Only a definitive missing-topic response allows recreation. A closed topic
// or revoked permission is not a missing topic.
func (b *Bot) EnsureLiveTopic(chatID int64, name string, t *LiveTopic, save func() error) error {
	if t.Pending {
		return fmt.Errorf("topic creation unresolved; reconcile topic ID before retrying")
	}
	if t.ID != 0 {
		if t.Name == name && time.Since(t.CheckedAt) < 5*time.Minute {
			return nil
		}
		err := b.EditForumTopic(chatID, t.ID, name)
		d := apiDescription(err)
		if err == nil || strings.Contains(d, "topic_not_modified") || strings.Contains(d, "topic is not modified") {
			t.Name = name
			t.CheckedAt = time.Now()
			return save()
		}
		if !IsTopicMissing(err) {
			return err
		}
		t.ID = 0
		t.Name = ""
		t.CheckedAt = time.Time{}
		if err := save(); err != nil {
			return err
		}
	}
	t.Pending = true
	if err := save(); err != nil {
		return fmt.Errorf("persist topic creation intent: %w", err)
	}
	id, err := b.CreateForumTopic(chatID, name)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && strings.HasPrefix(apiErr.Status, "4") {
			t.Pending = false
			if e := save(); e != nil {
				return e
			}
		}
		return err
	}
	if id <= 0 {
		return fmt.Errorf("topic creation returned no ID; reconciliation required")
	}
	t.ID = id
	t.Name = name
	t.Pending = false
	t.CheckedAt = time.Now()
	return save()
}
