from pathlib import Path

p = Path("internal/central/server.go")
s = p.read_text()

# Hourly infrastructure reports, aligned to the top of each hour.
s = s.replace("next := nextBoundary(time.Now(), 10*time.Minute)", "next := nextBoundary(time.Now(), time.Hour)")

start = s.index("func formatGlobal(")
end = s.index("func formatDaily(")

new = r'''func formatGlobal(nodes []model.Node, now time.Time) string {
	var stable, warning, offline int
	var rx, tx float64
	var todayRX, todayTX uint64
	var blocks []string
	date := now.Format("2006-01-02")

	for _, n := range nodes {
		on := !n.LastSeen.IsZero() && now.Sub(n.LastSeen) < 150*time.Second
		cpu := n.Latest.CPUPercent
		ram := pct(n.Latest.MemUsed, n.Latest.MemTotal)
		disk := pct(n.Latest.DiskUsed, n.Latest.DiskTotal)
		if !on {
			offline++
		} else {
			worst := math.Max(cpu, math.Max(ram, disk))
			if worst >= 85 {
				warning++
			} else {
				stable++
			}
			rx += n.Latest.RXBps
			tx += n.Latest.TXBps
		}
		if ds, ok := n.Daily[date]; ok {
			todayRX += ds.RXBytes
			todayTX += ds.TXBytes
		}
		blocks = append(blocks, formatNodeBlock(n, on, date))
	}

	head := fmt.Sprintf(`╭━━━━━━━━━━━━━━━━━━━━━━━━━━╮
┃ 💠 MARZWATCH CORE
┃ GLOBAL INFRASTRUCTURE
╰━━━━━━━━━━━━━━━━━━━━━━━━━━╯

🟢 LIVE • %s

🛰 NODES        %d
🟢 STABLE       %d
🟡 WARNING      %d
🔴 OFFLINE      %d

`, now.Format("15:04:05"), len(nodes), stable, warning, offline)

	foot := fmt.Sprintf(`

━━━━━━━━━━━━━━━━━━━━━━━━━━
📦 GLOBAL TRAFFIC TODAY
⬇️ DOWNLOAD    %s
⬆️ UPLOAD      %s
↕️ TOTAL       %s

━━━━━━━━━━━━━━━━━━━━━━━━━━
📡 GLOBAL LIVE NETWORK
⬇️ DOWNLOAD    %s
⬆️ UPLOAD      %s
↕️ TOTAL       %s

💚 MONITORING ACTIVE
⚡ Powered by Only :)`,
		bytes(todayRX), bytes(todayTX), bytes(todayRX+todayTX),
		rate(rx), rate(tx), rate(rx+tx))

	return head + strings.Join(blocks, "\n\n") + foot
}

func formatNodeBlock(n model.Node, on bool, date string) string {
	flag := flagEmoji(n.Location.CountryCode)
	cpu := n.Latest.CPUPercent
	ram := pct(n.Latest.MemUsed, n.Latest.MemTotal)
	disk := pct(n.Latest.DiskUsed, n.Latest.DiskTotal)
	state := "🟢 STABLE"
	worst := math.Max(cpu, math.Max(ram, disk))
	if !on {
		state = "🔴 OFFLINE"
	} else if worst >= 95 {
		state = "🔴 CRITICAL"
	} else if worst >= 85 {
		state = "🟠 HIGH LOAD"
	} else if worst >= 70 {
		state = "🟡 WATCH"
	}
	var todayRX, todayTX uint64
	if ds, ok := n.Daily[date]; ok {
		todayRX, todayTX = ds.RXBytes, ds.TXBytes
	}

	return fmt.Sprintf(`╭━━━━━━━━━━━━━━━━━━━━━━━━━━╮
┃ 🛰 %s %s
┃ 🌐 %s
┃ 📍 %s
┣━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃ %s
┃
┃ ⚡ CPU
┃ %s %s  %.1f%%
┃
┃ 🧠 RAM
┃ %s %s  %.1f%%
┃
┃ 💽 DISK
┃ %s %s  %.1f%%
┃
┃ 📡 NETWORK
┃ ⬇️ %s   ⬆️ %s
┃
┃ 📦 TODAY  %s
┃ ⏱ %s   🛡 %d/100
╰━━━━━━━━━━━━━━━━━━━━━━━━━━╯`,
		flag, n.Name, displayIP(n.Location.PublicIP), displayPlace(n.Location), state,
		statusEmoji(cpu), hudBar(cpu), cpu,
		statusEmoji(ram), hudBar(ram), ram,
		statusEmoji(disk), hudBar(disk), disk,
		rate(n.Latest.RXBps), rate(n.Latest.TXBps),
		bytes(todayRX+todayTX), durationLong(time.Duration(n.Latest.UptimeSeconds)*time.Second), health(n, on))
}

'''

s = s[:start] + new + s[end:]

# Brand every Telegram message family consistently.
repls = {
    "💠 MARZWATCH • END OF DAY`": "💠 MARZWATCH • END OF DAY\n⚡ Powered by Only :)`",
    "💚 MONITORING ACTIVE`, flagEmoji": "💚 MONITORING ACTIVE\n⚡ Powered by Only :)`, flagEmoji",
    "⚠️ MONITORING CONTINUES`, icon": "⚠️ MONITORING CONTINUES\n⚡ Powered by Only :)`, icon",
    "✅ MONITORING ACTIVE`, reason": "✅ MONITORING ACTIVE\n⚡ Powered by Only :)`, reason",
    "🛡 سایر Nodeها بدون تأثیر به مانیتورینگ ادامه می‌دهند.`, flagEmoji": "🛡 سایر Nodeها بدون تأثیر به مانیتورینگ ادامه می‌دهند.\n\n⚡ Powered by Only :)`, flagEmoji",
}
for old, new_text in repls.items():
    s = s.replace(old, new_text)

p.write_text(s)
