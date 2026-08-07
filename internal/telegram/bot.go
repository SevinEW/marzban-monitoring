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

func New(token string, chatID int64) *Bot {
	return &Bot{token: token, chatID: chatID, client: &http.Client{Timeout: 10 * time.Second}}
}
func (b *Bot) Enabled() bool { return b != nil && b.token != "" && b.chatID != 0 }
func (b *Bot) Send(text string) error {
	if !b.Enabled() {
		return nil
	}
	for _, chunk := range split(text, 3800) {
		if err := b.sendOne(chunk); err != nil {
			return err
		}
	}
	return nil
}
func (b *Bot) sendOne(text string) error {
	payload := map[string]any{"chat_id": b.chatID, "text": text, "disable_web_page_preview": true}
	data, _ := json.Marshal(payload)
	u := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.token)
	req, _ := http.NewRequest(http.MethodPost, u, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("telegram %s: %s", resp.Status, string(body))
	}
	return nil
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
