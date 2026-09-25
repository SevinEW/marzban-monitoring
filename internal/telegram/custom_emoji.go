package telegram

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type customEmoji struct{ Original, Alternative, ID string }

// Ordered replacements selected by the operator. IDs are strings: converting
// Telegram's 64-bit identifiers through floating point loses precision.
var monitoringEmoji = []customEmoji{
	{"💠", "#️⃣", "5951584964305755220"},
	{"🛰", "📍", "5778661935927004845"},
	{"🌐", "🌐", "5776233299424843260"},
	{"📍", "📍", "5778661935927004845"},
	{"🟢", "🟢", "5332440771180116150"},
	{"🟡", "🟡", "5332345843812943191"},
	{"🟠", "🟠", "5332369556327383798"},
	{"🔴", "🔴", "5332667755906743671"},
	{"⚡", "🔲", "5316767550753743074"},
	{"🧠", "📲", "5395355393457140432"},
	{"💽", "🔲", "5316950202827939853"},
	{"📡", "📶", "5870791093255147680"},
	{"⬇️", "⬇️", "5899757765743615694"},
	{"⬆️", "⬆️", "5963103826075456248"},
	{"↕️", "↕️", "5345992410906248539"},
	{"📦", "📦", "5884479287171485878"},
	{"⏱", "🕒", "5778605968208170641"},
	{"🛡", "🛡", "5931409969613116639"},
	{"🕒", "🕒", "5778605968208170641"},
	{"🌙", "🌙", "5345780046248298257"},
	{"📅", "📅", "5967412305338568701"},
	{"🔥", "🔥", "5116414868357907335"},
	{"🏆", "🏆", "5879810503101910670"},
	{"💚", "💚", "5800910665384203401"},
	{"🖥", "🖥", "5316581935152110749"},
	{"⚠️", "⚠️", "5881702736843511327"},
	{"✨", "✨", "5890925363067886150"},
	{"✅", "✅", "5776375003280838798"},
	{"🇩🇪", "🇩🇪", "5927115483353452394"},
	{"🇫🇮", "🇫🇮", "5929232623057506388"},
	{"🇬🇧", "🇬🇧", "5931790087103712847"},
	{"🇮🇹", "🇮🇹", "5929167717511728417"},
	{"🇳🇱", "🇳🇱", "5929336432417050197"},
	{"🇵🇱", "🇵🇱", "5224670399521892983"},
	{"🇹🇷", "🇹🇷", "6001312958947267486"},
	{"🇺🇸", "🇺🇸", "5927014272449121774"},
}

func EmojiRevision() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprint(monitoringEmoji))))
}

type emojiEntity struct {
	Type          string `json:"type"`
	Offset        int    `json:"offset"`
	Length        int    `json:"length"`
	CustomEmojiID string `json:"custom_emoji_id"`
}

func emojiText(text string) (string, []emojiEntity) {
	var out strings.Builder
	var entities []emojiEntity
	offset := 0
	for len(text) > 0 {
		matched := false
		for _, e := range monitoringEmoji {
			original := strings.TrimSuffix(e.Original, "\ufe0f")
			if !strings.HasPrefix(text, original) {
				continue
			}
			text = strings.TrimPrefix(text[len(original):], "\ufe0f")
			length := len(utf16.Encode([]rune(e.Alternative)))
			entities = append(entities, emojiEntity{Type: "custom_emoji", Offset: offset, Length: length, CustomEmojiID: e.ID})
			out.WriteString(e.Alternative)
			offset += length
			matched = true
			break
		}
		if matched {
			continue
		}
		r, size := utf8.DecodeRuneInString(text)
		out.WriteRune(r)
		text = text[size:]
		if r > 0xffff {
			offset += 2
		} else {
			offset++
		}
	}
	return out.String(), entities
}

// Apply to sends and edits equally; topic names do not support text entities.
// Do not reinterpret node names or IPs as Markdown/HTML markup.
func customEmojiPayload(method string, payload map[string]any) map[string]any {
	if method != "sendMessage" && method != "editMessageText" {
		return payload
	}
	if _, ok := payload["parse_mode"]; ok {
		return payload
	}
	if _, ok := payload["entities"]; ok {
		return payload
	}
	text, ok := payload["text"].(string)
	if !ok {
		return payload
	}
	rendered, entities := emojiText(text)
	if len(entities) == 0 {
		return payload
	}
	result := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		result[k] = v
	}
	result["text"], result["entities"] = rendered, entities
	return result
}

func emojiPermissionError(err error) bool {
	d := apiDescription(err)
	return strings.Contains(d, "custom_emoji_invalid") || strings.Contains(d, "custom_emoji_not_allowed") || strings.Contains(d, "premium_account_required") || strings.Contains(d, "not enough rights to send custom emoji")
}

func (b *Bot) call(method string, payload map[string]any, out any) error {
	if b == nil {
		return b.callRaw(method, payload, out)
	}
	rendered := payload
	if !b.plainEmoji.Load() {
		rendered = customEmojiPayload(method, payload)
	}
	err := b.callRaw(method, rendered, out)
	if _, custom := rendered["entities"]; custom && emojiPermissionError(err) {
		// The next regular, paced attempt uses plain emojis. Do not resend a
		// creation request here or retry any timeout with an unknown outcome.
		b.plainEmoji.Store(true)
	}
	return err
}
