package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Bot struct {
	token  string
	chatID int64
	client *http.Client
}

type apiEnvelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
}

type Chat struct {
	ID      int64 `json:"id"`
	IsForum bool  `json:"is_forum"`
}

type User struct {
	ID int64 `json:"id"`
}

type ChatMember struct {
	Status          string `json:"status"`
	CanManageTopics bool   `json:"can_manage_topics"`
	CanDelete       bool   `json:"can_delete_messages"`
}

type ForumTopic struct {
	MessageThreadID int `json:"message_thread_id"`
}

type Message struct {
	MessageID int `json:"message_id"`
}

func New(token string, chatID int64) *Bot {
	return &Bot{token: token, chatID: chatID, client: &http.Client{Timeout: 10 * time.Second}}
}

func (b *Bot) Enabled() bool { return b != nil && b.token != "" }

func (b *Bot) Send(text string) error {
	if !b.Enabled() || b.chatID == 0 {
		return nil
	}
	for _, chunk := range split(text, 3800) {
		if _, err := b.SendMessage(b.chatID, 0, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) call(method string, payload map[string]any, out any) error {
	if !b.Enabled() {
		return fmt.Errorf("telegram bot disabled")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	u := fmt.Sprintf("https://api.telegram.org/bot%s/%s", b.token, method)
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var env apiEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("telegram %s: invalid response", resp.Status)
	}
	if resp.StatusCode/100 != 2 || !env.OK {
		return fmt.Errorf("telegram %s: %s", resp.Status, env.Description)
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) ValidateForum(chatID int64) error {
	var chat Chat
	if err := b.call("getChat", map[string]any{"chat_id": chatID}, &chat); err != nil {
		return err
	}
	if !chat.IsForum {
		return fmt.Errorf("telegram group is not a forum; enable Topics first")
	}
	var me User
	if err := b.call("getMe", map[string]any{}, &me); err != nil {
		return err
	}
	var member ChatMember
	if err := b.call("getChatMember", map[string]any{"chat_id": chatID, "user_id": me.ID}, &member); err != nil {
		return err
	}
	if member.Status != "administrator" && member.Status != "creator" {
		return fmt.Errorf("bot is not an administrator in the forum group")
	}
	if !member.CanManageTopics {
		return fmt.Errorf("bot needs Manage Topics permission")
	}
	if !member.CanDelete {
		return fmt.Errorf("bot needs Delete Messages permission so stale-node topics can be removed")
	}
	return nil
}

func (b *Bot) CreateForumTopic(chatID int64, name string) (int, error) {
	var topic ForumTopic
	err := b.call("createForumTopic", map[string]any{"chat_id": chatID, "name": name}, &topic)
	return topic.MessageThreadID, err
}

func (b *Bot) EditForumTopic(chatID int64, threadID int, name string) error {
	return b.call("editForumTopic", map[string]any{"chat_id": chatID, "message_thread_id": threadID, "name": name}, nil)
}

func (b *Bot) DeleteForumTopic(chatID int64, threadID int) error {
	return b.call("deleteForumTopic", map[string]any{"chat_id": chatID, "message_thread_id": threadID}, nil)
}

func (b *Bot) SendMessage(chatID int64, threadID int, text string) (int, error) {
	payload := map[string]any{"chat_id": chatID, "text": text, "disable_web_page_preview": true}
	if threadID != 0 {
		payload["message_thread_id"] = threadID
	}
	var msg Message
	if err := b.call("sendMessage", payload, &msg); err != nil {
		return 0, err
	}
	return msg.MessageID, nil
}

func (b *Bot) EditMessage(chatID int64, messageID int, text string) error {
	return b.call("editMessageText", map[string]any{
		"chat_id":                  chatID,
		"message_id":               messageID,
		"text":                     text,
		"disable_web_page_preview": true,
	}, nil)
}

func split(s string, max int) []string {
	if len(s) <= max {
		return []string{s}
	}
	paras := strings.Split(s, "\n\n")
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
	}
	for _, p := range paras {
		need := len(p)
		if cur.Len() > 0 {
			need += 2
		}
		if cur.Len()+need <= max {
			if cur.Len() > 0 {
				cur.WriteString("\n\n")
			}
			cur.WriteString(p)
			continue
		}
		flush()
		if len(p) <= max {
			cur.WriteString(p)
			continue
		}
		for _, line := range strings.Split(p, "\n") {
			if cur.Len()+len(line)+1 > max {
				flush()
			}
			cur.WriteString(line)
			cur.WriteByte('\n')
		}
	}
	flush()
	return out
}
