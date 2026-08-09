from pathlib import Path

# Telegram forum checks twice per metric interval so a freshly received sample
# is reflected quickly. Agents send a new signed metric every 10 seconds.
forum = Path("internal/central/forum.go")
f = forum.read_text()

f = f.replace(
    "\tt := time.NewTicker(30 * time.Second)\n",
    "\tt := time.NewTicker(5 * time.Second)\n",
    1,
)
f = f.replace(
    "\tt := time.NewTicker(10 * time.Second)\n",
    "\tt := time.NewTicker(5 * time.Second)\n",
    1,
)

old_stale_start = "\t// Remove agents that have been silent for more than six hours. Central itself\n"
old_stale_end = "\tnodes := s.store.Nodes()\n"
if old_stale_start in f:
    start = f.index(old_stale_start)
    end = f.index(old_stale_end, start)
    f = (
        f[:start]
        + "\t// Stale-node deletion is owned exclusively by staleLoop. Forum sync only\n"
          "\t// reconciles Telegram topics with the current store to avoid duplicate GC.\n"
        + f[end:]
    )

f = f.replace(
    "\t\ttext := formatNodeBlock(n, on, date)\n",
    "\t\ttext := appendNodeLastUpdate(formatNodeBlock(n, on, date), n.LastSeen, s.tz)\n",
    1,
)

if "func appendNodeLastUpdate(" not in f:
    anchor = "func forumTopicName(n model.Node) string {\n"
    if anchor not in f:
        raise SystemExit("forumTopicName anchor not found")
    helper = '''func appendNodeLastUpdate(text string, lastSeen time.Time, tz *time.Location) string {
\tstamp := "NEVER"
\tif !lastSeen.IsZero() {
\t\tstamp = lastSeen.In(tz).Format("2006-01-02 15:04:05")
\t}
\tline := "┃ 🕒 LAST UPDATE  " + stamp
\tif i := strings.LastIndex(text, "\\n╰"); i >= 0 {
\t\treturn text[:i] + "\\n" + line + text[i:]
\t}
\treturn text + "\\n" + line
}

'''
    f = f.replace(anchor, helper + anchor, 1)

forum.write_text(f)

# Agent samples every 5s and sends one signed metric every 10s in the healthy
# path. Existing exponential backoff still protects Central during failures.
agent = Path("internal/agent/agent.go")
a = agent.read_text()
a = a.replace("time.NewTicker(15 * time.Second)", "time.NewTicker(5 * time.Second)", 1)
a = a.replace("time.Sleep(17 * time.Second)", "time.Sleep(5 * time.Second)", 1)
a = a.replace("time.Sleep(7 * time.Second)", "time.Sleep(5 * time.Second)", 1)
a = a.replace("\tdelay := time.Minute\n", "\tdelay := 10 * time.Second\n", 1)
a = a.replace("\tdelay := 15 * time.Second\n", "\tdelay := 10 * time.Second\n", 1)
a = a.replace("\t\t\t\t\tdelay = time.Minute\n", "\t\t\t\t\tdelay = 10 * time.Second\n", 1)
a = a.replace("\t\t\t\t\tdelay = 15 * time.Second\n", "\t\t\t\t\tdelay = 10 * time.Second\n", 1)
a = a.replace("\t\t\tdelay = time.Minute\n", "\t\t\tdelay = 10 * time.Second\n", 1)
a = a.replace("\t\t\tdelay = 15 * time.Second\n", "\t\t\tdelay = 10 * time.Second\n", 1)
agent.write_text(a)

# Central's local node follows the same 10s reporting cadence.
server = Path("internal/central/server.go")
s = server.read_text()
s = s.replace("\tticker := time.NewTicker(15 * time.Second)\n", "\tticker := time.NewTicker(5 * time.Second)\n", 1)
s = s.replace("time.Since(lastSend) >= 55*time.Second", "time.Since(lastSend) >= 10*time.Second", 1)
s = s.replace("time.Since(lastSend) >= 15*time.Second", "time.Since(lastSend) >= 10*time.Second", 1)
server.write_text(s)
