from pathlib import Path

# Telegram Forum writes share a per-supergroup flood limit. Keep telemetry fast,
# but serialize actual group mutations so live cards remain reliable instead of
# entering repeated 429/recreate loops.

bot = Path("internal/telegram/bot.go")
s = bot.read_text()

if '"sync"' not in s:
    s = s.replace('\t"strings"\n', '\t"strings"\n\t"sync"\n', 1)

old = '''type Bot struct {
\ttoken  string
\tchatID int64
\tclient *http.Client
}
'''
new = '''type Bot struct {
\ttoken  string
\tchatID int64
\tclient *http.Client

\tgroupMu           sync.Mutex
\tgroupNext         time.Time
\tgroupBlockedUntil time.Time
}
'''
if old in s:
    s = s.replace(old, new, 1)

old = '''type apiEnvelope struct {
\tOK          bool            `json:"ok"`
\tResult      json.RawMessage `json:"result"`
\tDescription string          `json:"description"`
}
'''
new = '''type apiParameters struct {
\tRetryAfter int `json:"retry_after"`
}

type apiEnvelope struct {
\tOK          bool            `json:"ok"`
\tResult      json.RawMessage `json:"result"`
\tDescription string          `json:"description"`
\tParameters  apiParameters   `json:"parameters"`
}

type APIError struct {
\tStatus      string
\tDescription string
\tRetryAfter  time.Duration
}

func (e *APIError) Error() string {
\tif e.RetryAfter > 0 {
\t\treturn fmt.Sprintf("telegram %s: %s; retry after %s", e.Status, e.Description, e.RetryAfter)
\t}
\treturn fmt.Sprintf("telegram %s: %s", e.Status, e.Description)
}

func RetryAfter(err error) (time.Duration, bool) {
\te, ok := err.(*APIError)
\tif !ok || e.RetryAfter <= 0 {
\t\treturn 0, false
\t}
\treturn e.RetryAfter, true
}

func IsRateLimit(err error) bool {
\t_, ok := RetryAfter(err)
\treturn ok
}
'''
if old in s:
    s = s.replace(old, new, 1)

old = '''\tif resp.StatusCode/100 != 2 || !env.OK {
\t\treturn fmt.Errorf("telegram %s: %s", resp.Status, env.Description)
\t}
'''
new = '''\tif resp.StatusCode/100 != 2 || !env.OK {
\t\tretry := time.Duration(env.Parameters.RetryAfter) * time.Second
\t\treturn &APIError{Status: resp.Status, Description: env.Description, RetryAfter: retry}
\t}
'''
if old in s:
    s = s.replace(old, new, 1)

anchor = 'func (b *Bot) ValidateForum(chatID int64) error {\n'
if 'func (b *Bot) withGroupWrite(' not in s:
    helper = '''const forumWriteSpacing = 3200 * time.Millisecond

func (b *Bot) withGroupWrite(chatID int64, fn func() error) error {
\t// Telegram applies a much tighter flood limit to writes targeting the same
\t// supergroup. Positive IDs (private/admin chat) do not share this queue.
\tif chatID >= 0 {
\t\treturn fn()
\t}

\tb.groupMu.Lock()
\tdefer b.groupMu.Unlock()

\twaitUntil := b.groupNext
\tif b.groupBlockedUntil.After(waitUntil) {
\t\twaitUntil = b.groupBlockedUntil
\t}
\tif d := time.Until(waitUntil); d > 0 {
\t\ttime.Sleep(d)
\t}

\terr := fn()
\tnow := time.Now()
\tb.groupNext = now.Add(forumWriteSpacing)
\tif retry, ok := RetryAfter(err); ok {
\t\t// Obey Telegram's explicit cooldown with a one-second safety margin.
\t\tb.groupBlockedUntil = now.Add(retry + time.Second)
\t\tif b.groupBlockedUntil.After(b.groupNext) {
\t\t\tb.groupNext = b.groupBlockedUntil
\t\t}
\t}
\treturn err
}

'''
    if anchor not in s:
        raise SystemExit("ValidateForum anchor not found")
    s = s.replace(anchor, helper + anchor, 1)

old = '''func (b *Bot) CreateForumTopic(chatID int64, name string) (int, error) {
\tvar topic ForumTopic
\terr := b.call("createForumTopic", map[string]any{"chat_id": chatID, "name": name}, &topic)
\treturn topic.MessageThreadID, err
}

func (b *Bot) EditForumTopic(chatID int64, threadID int, name string) error {
\treturn b.call("editForumTopic", map[string]any{"chat_id": chatID, "message_thread_id": threadID, "name": name}, nil)
}

func (b *Bot) DeleteForumTopic(chatID int64, threadID int) error {
\treturn b.call("deleteForumTopic", map[string]any{"chat_id": chatID, "message_thread_id": threadID}, nil)
}

func (b *Bot) SendMessage(chatID int64, threadID int, text string) (int, error) {
\tpayload := map[string]any{"chat_id": chatID, "text": text, "disable_web_page_preview": true}
\tif threadID != 0 {
\t\tpayload["message_thread_id"] = threadID
\t}
\tvar msg Message
\tif err := b.call("sendMessage", payload, &msg); err != nil {
\t\treturn 0, err
\t}
\treturn msg.MessageID, nil
}

func (b *Bot) EditMessage(chatID int64, messageID int, text string) error {
\treturn b.call("editMessageText", map[string]any{
\t\t"chat_id":                  chatID,
\t\t"message_id":               messageID,
\t\t"text":                     text,
\t\t"disable_web_page_preview": true,
\t}, nil)
}
'''
new = '''func (b *Bot) CreateForumTopic(chatID int64, name string) (int, error) {
\tvar topic ForumTopic
\terr := b.withGroupWrite(chatID, func() error {
\t\treturn b.call("createForumTopic", map[string]any{"chat_id": chatID, "name": name}, &topic)
\t})
\treturn topic.MessageThreadID, err
}

func (b *Bot) EditForumTopic(chatID int64, threadID int, name string) error {
\treturn b.withGroupWrite(chatID, func() error {
\t\treturn b.call("editForumTopic", map[string]any{"chat_id": chatID, "message_thread_id": threadID, "name": name}, nil)
\t})
}

func (b *Bot) DeleteForumTopic(chatID int64, threadID int) error {
\treturn b.withGroupWrite(chatID, func() error {
\t\treturn b.call("deleteForumTopic", map[string]any{"chat_id": chatID, "message_thread_id": threadID}, nil)
\t})
}

func (b *Bot) SendMessage(chatID int64, threadID int, text string) (int, error) {
\tpayload := map[string]any{"chat_id": chatID, "text": text, "disable_web_page_preview": true}
\tif threadID != 0 {
\t\tpayload["message_thread_id"] = threadID
\t}
\tvar msg Message
\terr := b.withGroupWrite(chatID, func() error {
\t\treturn b.call("sendMessage", payload, &msg)
\t})
\tif err != nil {
\t\treturn 0, err
\t}
\treturn msg.MessageID, nil
}

func (b *Bot) EditMessage(chatID int64, messageID int, text string) error {
\treturn b.withGroupWrite(chatID, func() error {
\t\treturn b.call("editMessageText", map[string]any{
\t\t\t"chat_id":                  chatID,
\t\t\t"message_id":               messageID,
\t\t\t"text":                     text,
\t\t\t"disable_web_page_preview": true,
\t\t}, nil)
\t})
}
'''
if old not in s:
    raise SystemExit("telegram write methods anchor not found")
s = s.replace(old, new, 1)
bot.write_text(s)

forum = Path("internal/central/forum.go")
f = forum.read_text()

old = '''\t} else if overview != st.OverviewLastText {
\t\tif err := s.bot.EditMessage(s.cfg.TelegramGroupID, st.OverviewMessageID, overview); err != nil {
\t\t\tmsgID, sendErr := s.bot.SendMessage(s.cfg.TelegramGroupID, st.OverviewTopicID, overview)
'''
new = '''\t} else if overview != st.OverviewLastText {
\t\tif err := s.bot.EditMessage(s.cfg.TelegramGroupID, st.OverviewMessageID, overview); err != nil {
\t\t\tif telegram.IsRateLimit(err) {
\t\t\t\tlog.Printf("overview edit rate-limited; preserving message for retry: %v", err)
\t\t\t\treturn nil
\t\t\t}
\t\t\tmsgID, sendErr := s.bot.SendMessage(s.cfg.TelegramGroupID, st.OverviewTopicID, overview)
'''
if old not in f:
    raise SystemExit("overview edit anchor not found")
f = f.replace(old, new, 1)

old = '''\t\t} else if text != fs.LastText {
\t\t\tif err := s.bot.EditMessage(s.cfg.TelegramGroupID, fs.MessageID, text); err != nil {
\t\t\t\tmsgID, sendErr := s.bot.SendMessage(s.cfg.TelegramGroupID, fs.TopicID, text)
'''
new = '''\t\t} else if text != fs.LastText {
\t\t\tif err := s.bot.EditMessage(s.cfg.TelegramGroupID, fs.MessageID, text); err != nil {
\t\t\t\tif telegram.IsRateLimit(err) {
\t\t\t\t\tlog.Printf("live card rate-limited for %s; preserving message for retry: %v", n.Name, err)
\t\t\t\t\tst.Nodes[n.ID] = fs
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t\tmsgID, sendErr := s.bot.SendMessage(s.cfg.TelegramGroupID, fs.TopicID, text)
'''
if old not in f:
    raise SystemExit("node edit anchor not found")
f = f.replace(old, new, 1)

# If orphan-topic deletion is rate-limited, keep its mapping so the next pass
# can retry rather than forgetting a topic that still exists in Telegram.
old = '''\t\tif fs.TopicID != 0 {
\t\t\tif err := s.bot.DeleteForumTopic(s.cfg.TelegramGroupID, fs.TopicID); err != nil {
\t\t\t\tlog.Printf("delete orphan topic %s: %v", fs.TopicName, err)
\t\t\t}
\t\t}
\t\tdelete(st.Nodes, id)
'''
new = '''\t\tif fs.TopicID != 0 {
\t\t\tif err := s.bot.DeleteForumTopic(s.cfg.TelegramGroupID, fs.TopicID); err != nil {
\t\t\t\tlog.Printf("delete orphan topic %s: %v", fs.TopicName, err)
\t\t\t\tcontinue
\t\t\t}
\t\t}
\t\tdelete(st.Nodes, id)
'''
if old in f:
    f = f.replace(old, new, 1)

forum.write_text(f)
