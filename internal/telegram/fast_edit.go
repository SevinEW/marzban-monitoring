package telegram

import "time"

const initialEditSpacing = 1500 * time.Millisecond

// Existing-message edits share the group lock and explicit Telegram cooldown.
// Creation/deletion retain conservative pacing. A flood response permanently
// slows edits for this process instead of retrying an aggressive rate in a loop.
func (b *Bot) withGroupEdit(chatID int64, fn func() error) error {
 if chatID>=0{return fn()}
 b.groupMu.Lock();defer b.groupMu.Unlock()
 if b.editSpacing==0{b.editSpacing=initialEditSpacing}
 until:=b.groupNext;if b.groupBlockedUntil.After(until){until=b.groupBlockedUntil}
 if d:=time.Until(until);d>0{time.Sleep(d)}
 started:=time.Now();err:=fn();now:=time.Now()
 b.groupNext=started.Add(b.editSpacing)
 if retry,ok:=RetryAfter(err);ok{
  b.editSpacing*=2;if b.editSpacing<forumWriteSpacing{b.editSpacing=forumWriteSpacing};if b.editSpacing>15*time.Second{b.editSpacing=15*time.Second}
  b.groupBlockedUntil=now.Add(retry+time.Second)
  if b.groupBlockedUntil.After(b.groupNext){b.groupNext=b.groupBlockedUntil}
 }
 return err
}
