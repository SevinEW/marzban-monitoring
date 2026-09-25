"""Run last in the release build: durable cards, ordering and capacity details."""
from pathlib import Path

def once(s, old, new):
    if s.count(old) != 1:
        raise SystemExit(f'Expected one anchor: {old[:100]!r}')
    return s.replace(old, new, 1)

p = Path('internal/central/forum.go')
s = p.read_text(encoding='utf-8')
s = once(s, '\t"sort"\n', '')
s = once(s, 'type forumNodeState struct {', 'type forumNodeState struct {\n\tSendPending bool `json:"send_pending,omitempty"`')
s = once(s, 'type forumState struct {', 'type forumState struct {\n\tOverviewTopicName string `json:"overview_topic_name,omitempty"`\n\tOverviewSendPending bool `json:"overview_send_pending,omitempty"`')
s = once(s, '\tnodes := s.store.Nodes()\n', '\tnodes := sortedForumNodes(s.store.Nodes())\n')
s = once(s, '"💠 Overview"', '"00 · 💠 Overview"')
s = once(s, '\t\tst.OverviewTopicID = topicID', '\t\tst.OverviewTopicID = topicID\n\t\tst.OverviewTopicName = "00 · 💠 Overview"')
s = once(s, '\toverview := formatOverviewLive', '''\tif st.OverviewTopicName != "00 · 💠 Overview" {
\t\tif err := s.bot.EditForumTopic(s.cfg.TelegramGroupID, st.OverviewTopicID, "00 · 💠 Overview"); err != nil {
\t\t\tlog.Printf("rename overview topic: %v", err)
\t\t} else {
\t\t\tst.OverviewTopicName = "00 · 💠 Overview"
\t\t\tchanged = true
\t\t}
\t}
\toverview := formatOverviewLive''')
start = s.index('\tif st.OverviewMessageID == 0 {')
end = s.index('\n\tdate := ', start)
s = s[:start] + '''\tif changed {
\t\tif err := saveForumState(*st); err != nil { return err }
\t}
\tcard := telegram.LiveCard{MessageID: st.OverviewMessageID, LastText: st.OverviewLastText, Pending: st.OverviewSendPending}
\terr := s.bot.SyncLiveCard(s.cfg.TelegramGroupID, st.OverviewTopicID, overview, &card, func() error {
\t\tst.OverviewMessageID, st.OverviewLastText, st.OverviewSendPending = card.MessageID, card.LastText, card.Pending
\t\treturn saveForumState(*st)
\t})
\tif err != nil { log.Printf("overview live card: %v", err) }
''' + s[end:]
s = once(s, '\tfor _, n := range nodes {\n\t\tseen[n.ID]', '\tfor index, n := range nodes {\n\t\tseen[n.ID]')
s = once(s, '\t\tname := forumTopicName(n)', '\t\tname := fmt.Sprintf("%02d · %s", index+1, forumTopicName(n))')
start = s.index('\t\tif fs.MessageID == 0 {')
end = s.index('\n\t\tst.Nodes[n.ID] = fs\n\t}', start)
s = s[:start] + '''\t\tst.Nodes[n.ID] = fs
\t\tif changed {
\t\t\tif err := saveForumState(*st); err != nil { return err }
\t\t}
\t\tcard := telegram.LiveCard{MessageID: fs.MessageID, LastText: fs.LastText, Pending: fs.SendPending}
\t\terr := s.bot.SyncLiveCard(s.cfg.TelegramGroupID, fs.TopicID, text, &card, func() error {
\t\t\tfs.MessageID, fs.LastText, fs.SendPending = card.MessageID, card.LastText, card.Pending
\t\t\tst.Nodes[n.ID] = fs
\t\t\treturn saveForumState(*st)
\t\t})
\t\tif err != nil { log.Printf("node live card %s: %v", n.Name, err) }
''' + s[end:]
s = once(s, '\tfor _, n := range nodes {', '\tfor _, n := range sortedForumNodes(nodes) {')
s = once(s, '\tsort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })\n', '')
s = once(s, '\tfor _, r := range rows {\n\t\tfmt.Fprintf(&list, "%s %s  •  🛡 %d/100\\n", r.status, r.name, r.health)', '\tfor i, r := range rows {\n\t\tfmt.Fprintf(&list, "%02d · %s %s  •  🛡 %d/100\\n", i+1, r.status, r.name, r.health)')
p.write_text(s, encoding='utf-8')

p = Path('internal/central/server.go')
s = p.read_text(encoding='utf-8')
start = s.index('func formatNodeBlock(')
end = s.index('\nfunc ', start+5)
b = s[start:end].replace('┃ %s %s  %.1f%%\n', '┃ %s %s  %.1f%%\n┃ %s\n')
b = once(b, 'statusEmoji(cpu), hudBar(cpu), cpu,', 'statusEmoji(cpu), hudBar(cpu), cpu, cpuUsageDetail(n),')
b = once(b, 'statusEmoji(ram), hudBar(ram), ram,', 'statusEmoji(ram), hudBar(ram), ram, fmt.Sprintf("Used %s / %s", bytes(n.Latest.MemUsed), bytes(n.Latest.MemTotal)),')
b = once(b, 'statusEmoji(disk), hudBar(disk), disk,', 'statusEmoji(disk), hudBar(disk), disk, fmt.Sprintf("Used %s / %s", bytes(n.Latest.DiskUsed), bytes(n.Latest.DiskTotal)),')
p.write_text(s[:start] + b + s[end:], encoding='utf-8')

p = Path('internal/telegram/bot.go')
s = p.read_text(encoding='utf-8')
s = once(s, 'resp, err := b.client.Do(req)\n\tif err != nil {\n\t\treturn err', 'resp, err := b.client.Do(req)\n\tif err != nil {\n\t\treturn fmt.Errorf("telegram transport failure (%s); request outcome unknown", method)')
p.write_text(s, encoding='utf-8')
