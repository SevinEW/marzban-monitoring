package telegram

import (
	"errors"
	"fmt"
	"strings"
)

// LiveCard keeps a durable send intent: Telegram sendMessage has no idempotency
// key, so an unanswered request must never be retried blindly after a restart.
type LiveCard struct {
	MessageID int
	LastText  string
	Pending   bool
}

func apiDescription(err error) string {
	var e *APIError
	if errors.As(err, &e) && strings.HasPrefix(e.Status, "400 ") {
		return strings.ToLower(e.Description)
	}
	return ""
}

func (b *Bot) DeleteMessage(chatID int64, messageID int) error {
	return b.withGroupWrite(chatID, func() error {
		return b.call("deleteMessage", map[string]any{"chat_id": chatID, "message_id": messageID}, nil)
	})
}

// SyncLiveCard edits in place. Only an explicit missing/uneditable-message
// response permits replacement. Unknown failures preserve the current card.
func (b *Bot) SyncLiveCard(chatID int64, topicID int, text string, card *LiveCard, save func() error) error {
	return b.SyncLiveCardLatest(chatID, topicID, func() string { return text }, card, save)
}

// SyncLiveCardLatest renders after the shared flood-control wait, so the end of
// a long forum round uses current telemetry rather than a round-start snapshot.
// An empty rendering cancels work for a node removed while this card was queued.
func (b *Bot) SyncLiveCardLatest(chatID int64, topicID int, render func() string, card *LiveCard, save func() error) error {
	text := render()
	if text == "" {
		return nil
	}

	if card.Pending {
		return fmt.Errorf("live card send unresolved; reconcile message ID before retrying")
	}
	if card.MessageID != 0 {
		if text == card.LastText {
			return nil
		}
		err := b.withGroupWrite(chatID, func() error {
			text = render()
			if text == "" || text == card.LastText {
				return nil
			}
			return b.call("editMessageText", map[string]any{
				"chat_id": chatID, "message_id": card.MessageID,
				"text": text, "disable_web_page_preview": true,
			}, nil)
		})
		if text == "" {
			return nil
		}
		desc := apiDescription(err)
		if err == nil || strings.Contains(desc, "message is not modified") {
			card.LastText = text
			return save()
		}
		switch {
		case strings.Contains(desc, "message to edit not found"):
			// The old card is confirmed absent.
		case strings.Contains(desc, "message can't be edited"):
			// Keep the old card if deletion is forbidden, too old, or uncertain.
			if err := b.DeleteMessage(chatID, card.MessageID); err != nil &&
				!strings.Contains(apiDescription(err), "message to delete not found") {
				return fmt.Errorf("remove old live card: %w", err)
			}
		default:
			return err
		}
		card.MessageID, card.LastText = 0, ""
	}
	card.Pending = true
	if err := save(); err != nil {
		return fmt.Errorf("persist live card send intent: %w", err)
	}
	var msg Message
	err := b.withGroupWrite(chatID, func() error {
		text = render()
		if text == "" {
			return nil
		}
		payload := map[string]any{"chat_id": chatID, "text": text, "disable_web_page_preview": true}
		if topicID != 0 {
			payload["message_thread_id"] = topicID
		}
		return b.call("sendMessage", payload, &msg)
	})
	if text == "" {
		card.Pending = false
		return save()
	}
	id := msg.MessageID
	if err != nil {
		// Explicit 4xx failures were rejected. Transport/5xx/invalid-response
		// failures may have been accepted remotely; leave the intent pending.
		var apiErr *APIError
		if errors.As(err, &apiErr) && strings.HasPrefix(apiErr.Status, "4") {
			card.Pending = false
			if saveErr := save(); saveErr != nil {
				return saveErr
			}
		}
		return err
	}
	if id <= 0 {
		return fmt.Errorf("live card send returned no message ID; reconciliation required")
	}
	card.MessageID, card.LastText, card.Pending = id, text, false
	return save()
}
