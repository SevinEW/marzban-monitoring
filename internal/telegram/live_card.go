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
	if card.Pending {
		return fmt.Errorf("live card send unresolved; reconcile message ID before retrying")
	}
	if card.MessageID != 0 {
		if text == card.LastText {
			return nil
		}
		err := b.EditMessage(chatID, card.MessageID, text)
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
	id, err := b.SendMessage(chatID, topicID, text)
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
