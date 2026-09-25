package telegram

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type topicEvent struct {
	ID      int `json:"update_id"`
	Message *struct {
		ID       int  `json:"message_id"`
		ThreadID int  `json:"message_thread_id"`
		From     User `json:"from"`
		Chat     Chat `json:"chat"`
		Edited   *struct {
			Name string `json:"name"`
		} `json:"forum_topic_edited"`
	} `json:"message"`
}

func (e topicEvent) ownRename(chatID int64, selfID int64, managed func(int) bool) bool {
	m := e.Message
	return m != nil && m.ID > 0 && m.Chat.ID == chatID && m.From.ID == selfID && m.Edited != nil && m.Edited.Name != "" && managed(m.ThreadID)
}

// Only known-topic rename service messages authored by this bot are removed.
// getUpdates is not a history API: old events outside its queue are untouched.
func (b *Bot) CleanTopicRenames(chatID int64, stop <-chan struct{}, path string, managed func(int) bool, logf func(string, ...any)) {
	pause := func(d time.Duration) bool {
		select {
		case <-stop:
			return false
		case <-time.After(d):
			return true
		}
	}
	var self User
	for {
		if err := b.call("getMe", map[string]any{}, &self); err == nil {
			break
		}
		if !pause(time.Minute) {
			return
		}
	}
	var webhook struct {
		URL string `json:"url"`
	}
	if err := b.call("getWebhookInfo", map[string]any{}, &webhook); err != nil || webhook.URL != "" {
		logf("topic cleanup unavailable: webhook state must be empty and readable")
		return
	}
	offset := 0
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &offset) != nil {
			logf("topic cleanup offset is invalid")
			return
		}
	} else if !os.IsNotExist(err) {
		logf("topic cleanup offset unavailable")
		return
	}
	for {
		select {
		case <-stop:
			return
		default:
		}
		var events []topicEvent
		err := b.call("getUpdates", map[string]any{"offset": offset, "timeout": 5, "limit": 100, "allowed_updates": []string{"message"}}, &events)
		if err != nil {
			if e, ok := err.(*APIError); ok && strings.HasPrefix(e.Status, "409 ") {
				logf("topic cleanup stopped: another update consumer or webhook is active")
				return
			}
			logf("topic cleanup poll: %v", err)
			if !pause(time.Minute) {
				return
			}
			continue
		}
		for _, event := range events {
			if event.ID < offset {
				continue
			}
			if event.ownRename(chatID, self.ID, managed) {
				err = b.DeleteMessage(chatID, event.Message.ID)
				if err != nil {
					desc := apiDescription(err)
					if !strings.Contains(desc, "message to delete not found") && !strings.Contains(desc, "message can't be deleted") {
						logf("topic rename cleanup: %v", err)
						if !pause(time.Minute) {
							return
						}
						break
					}
				}
			}
			next := event.ID + 1
			data, _ := json.Marshal(next)
			if err = os.WriteFile(path+".tmp", data, 0600); err == nil {
				err = os.Rename(path+".tmp", path)
			}
			if err != nil {
				logf("topic cleanup offset save failed: %v", fmt.Errorf("persist offset: %w", err))
				return
			}
			offset = next
		}
	}
}
