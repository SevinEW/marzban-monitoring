from pathlib import Path

# Wire the new CLI command without duplicating the large main.go file.
main = Path("cmd/marzwatch/main.go")
m = main.read_text()
if 'case "forum-setup":' not in m:
    m = m.replace(
        'case "fleet-update":\n\t\tmustRoot()\n\t\tfleetUpdate()',
        'case "fleet-update":\n\t\tmustRoot()\n\t\tfleetUpdate()\n\tcase "forum-setup":\n\t\tmustRoot()\n\t\tforumSetup()',
        1,
    )
    m = m.replace(
        '  join-key\\n  fleet-update\\n  doctor',
        '  join-key\\n  fleet-update\\n  forum-setup\\n  doctor',
        1,
    )
main.write_text(m)

server = Path("internal/central/server.go")
s = server.read_text()

# Run stale-node GC regardless of Telegram status; run forum sync independently.
anchor = '\tgo s.supervise("daily-loop", s.dailyLoop)\n'
if 'go s.supervise("forum-loop", s.forumLoop)' not in s:
    if anchor not in s:
        raise SystemExit("server worker anchor not found")
    s = s.replace(
        anchor,
        anchor + '\tgo s.supervise("stale-loop", s.staleLoop)\n\tgo s.supervise("forum-loop", s.forumLoop)\n',
        1,
    )

# When Forum mode is enabled, live cards replace legacy DM report/alert spam.
s = s.replace(
    '\t_ = s.bot.Send(formatNewNode(n))\n',
    '\tif s.cfg.TelegramGroupID == 0 {\n\t\t_ = s.bot.Send(formatNewNode(n))\n\t}\n',
    1,
)
s = s.replace(
    '\tif wasOffline {\n\t\t_ = s.bot.Send(formatRecovery(n, "ارتباط سرور دوباره برقرار شد"))\n\t}\n',
    '\tif wasOffline && s.cfg.TelegramGroupID == 0 {\n\t\t_ = s.bot.Send(formatRecovery(n, "ارتباط سرور دوباره برقرار شد"))\n\t}\n',
    1,
)
s = s.replace(
    '\t\t\tif err := s.bot.Send(msg); err != nil {\n\t\t\t\tlog.Printf("telegram alert: %v", err)\n\t\t\t}\n',
    '\t\t\tif s.cfg.TelegramGroupID == 0 {\n\t\t\t\tif err := s.bot.Send(msg); err != nil {\n\t\t\t\t\tlog.Printf("telegram alert: %v", err)\n\t\t\t\t}\n\t\t\t}\n',
    1,
)
s = s.replace(
    '\t\t\t\tif changed {\n\t\t\t\t\t_ = s.bot.Send(formatOffline(n, now.Sub(n.LastSeen)))\n\t\t\t\t}\n',
    '\t\t\t\tif changed && s.cfg.TelegramGroupID == 0 {\n\t\t\t\t\t_ = s.bot.Send(formatOffline(n, now.Sub(n.LastSeen)))\n\t\t\t\t}\n',
    1,
)
s = s.replace(
    '\t\tcase <-timer.C:\n\t\t\tif err := s.bot.Send(formatGlobal(s.store.Nodes(), time.Now().In(s.tz))); err != nil {\n\t\t\t\tlog.Printf("telegram report: %v", err)\n\t\t\t}\n',
    '\t\tcase <-timer.C:\n\t\t\tif s.cfg.TelegramGroupID == 0 {\n\t\t\t\tif err := s.bot.Send(formatGlobal(s.store.Nodes(), time.Now().In(s.tz))); err != nil {\n\t\t\t\t\tlog.Printf("telegram report: %v", err)\n\t\t\t\t}\n\t\t\t}\n',
    1,
)
s = s.replace(
    '\t\tcase <-timer.C:\n\t\t\tdate := next.Add(-time.Second).Format("2006-01-02")\n\t\t\tif err := s.bot.Send(formatDaily(s.store.Nodes(), date)); err != nil {\n\t\t\t\tlog.Printf("telegram daily: %v", err)\n\t\t\t}\n',
    '\t\tcase <-timer.C:\n\t\t\tif s.cfg.TelegramGroupID == 0 {\n\t\t\t\tdate := next.Add(-time.Second).Format("2006-01-02")\n\t\t\t\tif err := s.bot.Send(formatDaily(s.store.Nodes(), date)); err != nil {\n\t\t\t\t\tlog.Printf("telegram daily: %v", err)\n\t\t\t\t}\n\t\t\t}\n',
    1,
)
server.write_text(s)

# Avoid an unprotected alert-map delete in the forum cleanup path.
forum = Path("internal/central/forum.go")
f = forum.read_text()
f = f.replace(
    '\t\tif _, ok := s.store.RemoveNode(n.ID); ok {\n\t\t\tdelete(s.alerts, n.ID)\n\t\t\t_ = s.store.Flush()',
    '\t\tif _, ok := s.store.RemoveNode(n.ID); ok {\n\t\t\ts.alertsMu.Lock()\n\t\t\tdelete(s.alerts, n.ID)\n\t\t\ts.alertsMu.Unlock()\n\t\t\t_ = s.store.Flush()',
    1,
)
forum.write_text(f)
