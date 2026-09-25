"""Decorate message payloads after all existing Telegram reliability transforms."""
from pathlib import Path

p=Path('internal/telegram/bot.go')
s=p.read_text(encoding='utf-8')
changes={
 '\t"sync"':'\t"sync"\n\t"sync/atomic"',
 'type Bot struct {':'type Bot struct {\n plainEmoji atomic.Bool',
 'func (b *Bot) call(method string, payload map[string]any, out any) error {':'func (b *Bot) callRaw(method string, payload map[string]any, out any) error {',
}
for old,new in changes.items():
 if s.count(old)!=1:raise SystemExit(f'Emoji integration anchor missing: {old}')
 s=s.replace(old,new,1)
p.write_text(s,encoding='utf-8')

p=Path('internal/central/forum.go')
s=p.read_text(encoding='utf-8')
s=s.replace('type forumState struct {','type forumState struct {\n EmojiRevision string `json:"emoji_revision,omitempty"`',1)
old='func (s *Server) syncForum(st *forumState) error {'
if s.count(old)!=1:raise SystemExit('Missing forum renderer anchor')
s=s.replace(old,old+'''
 // Force one in-place edit for unchanged/offline cards when emoji selection
 // changes. Never reset their message IDs or unresolved send intents.
 if revision:=telegram.EmojiRevision();st.EmojiRevision!=revision {
  st.OverviewLastText=""
  for id,fs:=range st.Nodes { fs.LastText="";st.Nodes[id]=fs }
  st.EmojiRevision=revision
  if err:=saveForumState(*st);err!=nil {return err}
 }
''',1)
p.write_text(s,encoding='utf-8')
