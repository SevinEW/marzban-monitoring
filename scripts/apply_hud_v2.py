from pathlib import Path

p = Path("internal/central/server.go")
s = p.read_text()
s = s.replace('_, _ = w.Write([]byte(`{\\"ok\\":true}`))', '_, _ = w.Write([]byte(`{"ok":true}`))')
start = s.index("func formatGlobal(")
end = s.index("func pct(")
new = r'''func formatGlobal(nodes []model.Node, now time.Time) string {
	var stable, warning, offline int
	var cpu, ram, disk, rx, tx float64
	var todayRX, todayTX uint64
	var healthSum int
	var blocks []string
	count := 0
	date := now.Format("2006-01-02")

	for _, n := range nodes {
		on := !n.LastSeen.IsZero() && now.Sub(n.LastSeen) < 150*time.Second
		c := n.Latest.CPUPercent
		r := pct(n.Latest.MemUsed, n.Latest.MemTotal)
		d := pct(n.Latest.DiskUsed, n.Latest.DiskTotal)
		if !on {
			offline++
		} else {
			worst := math.Max(c, math.Max(r, d))
			if worst >= 85 {
				warning++
			} else {
				stable++
			}
			cpu += c
			ram += r
			disk += d
			rx += n.Latest.RXBps
			tx += n.Latest.TXBps
			healthSum += health(n, true)
			count++
		}
		if ds, ok := n.Daily[date]; ok {
			todayRX += ds.RXBytes
			todayTX += ds.TXBytes
		}
		blocks = append(blocks, formatNodeBlock(n, on, date))
	}
	globalHealth := 0
	if count > 0 {
		cpu /= float64(count)
		ram /= float64(count)
		disk /= float64(count)
		globalHealth = healthSum / count
	}

	head := fmt.Sprintf(`╭━━━━━━━━━━━━━━━━━━━━━━╮
┃ 💠 MARZWATCH CORE
┃ GLOBAL INFRASTRUCTURE
╰━━━━━━━━━━━━━━━━━━━━━━╯

🟢 LIVE • %s

🛰 NODES        %d
🟢 STABLE       %d
🟡 WARNING      %d
🔴 OFFLINE      %d

━━━━━━━━━━━━━━━━━━━━━━
⚡ GLOBAL LOAD

CPU   %s  %s  %.1f%%
RAM   %s  %s  %.1f%%
DISK  %s  %s  %.1f%%

🛡 HEALTH  %s  %d/100

━━━━━━━━━━━━━━━━━━━━━━
📡 LIVE NETWORK

⬇️ DOWNLOAD    %s
⬆️ UPLOAD      %s
↕️ TOTAL       %s

📦 TRAFFIC TODAY
⬇️ DOWNLOAD    %s
⬆️ UPLOAD      %s
↕️ TOTAL       %s

━━━━━━━━━━━━━━━━━━━━━━
`, now.Format("15:04:05"), len(nodes), stable, warning, offline,
		statusEmoji(cpu), hudBar(cpu), cpu,
		statusEmoji(ram), hudBar(ram), ram,
		statusEmoji(disk), hudBar(disk), disk,
		healthEmoji(globalHealth), globalHealth,
		rate(rx), rate(tx), rate(rx+tx), bytes(todayRX), bytes(todayTX), bytes(todayRX+todayTX))

	return head + strings.Join(blocks, "\n") + "\n💚 MONITORING ACTIVE • MARZWATCH CORE"
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

	return fmt.Sprintf(`
┏━━ 🛰 %s %s ━━━━━━━━━┓
🌐 %s
📍 %s
%s

━━━━━━━━━━━━━━━━━━━━━━
⚡ PERFORMANCE

CPU   %s  %s  %.1f%%
      %d Cores • Load %.2f / %.2f / %.2f

RAM   %s  %s  %.1f%%
      %s / %s

DISK  %s  %s  %.1f%%
      %s / %s

━━━━━━━━━━━━━━━━━━━━━━
📡 LIVE NETWORK

⬇️ DOWNLOAD    %s
⬆️ UPLOAD      %s
↕️ TOTAL       %s

📦 TRAFFIC TODAY
⬇️ DOWNLOAD    %s
⬆️ UPLOAD      %s
↕️ TOTAL       %s

━━━━━━━━━━━━━━━━━━━━━━
⏱ UPTIME      %s
🛡 HEALTH      %s  %d/100
┗━━━━━━━━━━━━━━━━━━━━━━┛
`, flag, n.Name, displayIP(n.Location.PublicIP), displayPlace(n.Location), state,
		statusEmoji(cpu), hudBar(cpu), cpu, n.Cores, n.Latest.Load1, n.Latest.Load5, n.Latest.Load15,
		statusEmoji(ram), hudBar(ram), ram, bytes(n.Latest.MemUsed), bytes(n.Latest.MemTotal),
		statusEmoji(disk), hudBar(disk), disk, bytes(n.Latest.DiskUsed), bytes(n.Latest.DiskTotal),
		rate(n.Latest.RXBps), rate(n.Latest.TXBps), rate(n.Latest.RXBps+n.Latest.TXBps),
		bytes(todayRX), bytes(todayTX), bytes(todayRX+todayTX),
		durationLong(time.Duration(n.Latest.UptimeSeconds)*time.Second), healthEmoji(health(n, on)), health(n, on))
}

func formatDaily(nodes []model.Node, date string) string {
	var totalRX, totalTX uint64
	var cpuGlobal, ramGlobal, diskGlobal float64
	count := 0
	cpuLow, cpuHigh, cpuPeak := 101.0, -1.0, -1.0
	ramLow, ramHigh, ramPeak := 101.0, -1.0, -1.0
	diskLow, diskHigh := 101.0, -1.0
	var cpuLowNode, cpuHighNode, cpuPeakNode string
	var ramLowNode, ramHighNode, ramPeakNode string
	var diskLowNode, diskHighNode string
	var busiest, lightest string
	highPressure, lowPressure := -1.0, 1e9
	var peakRate float64
	var peakRateNode string
	var onlineAtEnd int

	for _, n := range nodes {
		d, ok := n.Daily[date]
		if !ok || d.Samples == 0 {
			continue
		}
		cpuAvg := d.CPUSum / float64(d.Samples)
		ramAvg := d.RAMSum / float64(d.Samples)
		diskAvg := d.DiskSum / float64(d.Samples)
		cpuGlobal += cpuAvg
		ramGlobal += ramAvg
		diskGlobal += diskAvg
		count++
		totalRX += d.RXBytes
		totalTX += d.TXBytes
		label := flagEmoji(n.Location.CountryCode) + " " + n.Name
		if !n.LastSeen.IsZero() && time.Since(n.LastSeen) < 150*time.Second {
			onlineAtEnd++
		}

		if cpuAvg < cpuLow {
			cpuLow, cpuLowNode = cpuAvg, label
		}
		if cpuAvg > cpuHigh {
			cpuHigh, cpuHighNode = cpuAvg, label
		}
		if d.CPUMax > cpuPeak {
			cpuPeak, cpuPeakNode = d.CPUMax, label
		}
		if ramAvg < ramLow {
			ramLow, ramLowNode = ramAvg, label
		}
		if ramAvg > ramHigh {
			ramHigh, ramHighNode = ramAvg, label
		}
		if d.RAMMax > ramPeak {
			ramPeak, ramPeakNode = d.RAMMax, label
		}
		if diskAvg < diskLow {
			diskLow, diskLowNode = diskAvg, label
		}
		if diskAvg > diskHigh {
			diskHigh, diskHighNode = diskAvg, label
		}
		if d.PeakTotalBps > peakRate {
			peakRate, peakRateNode = d.PeakTotalBps, label
		}

		pressure := cpuAvg + ramAvg
		if pressure > highPressure {
			highPressure, busiest = pressure, label
		}
		if pressure < lowPressure {
			lowPressure, lightest = pressure, label
		}
	}
	if count == 0 {
		return fmt.Sprintf(`╭━━━━━━━━━━━━━━━━━━━━━━╮
┃ 🌙 DAILY SYSTEM REPORT
┃ MARZWATCH • 00:00
╰━━━━━━━━━━━━━━━━━━━━━━╯

📅 %s

🟡 هنوز داده کافی برای تحلیل 24 ساعته ثبت نشده است.

💠 MARZWATCH • END OF DAY`, date)
	}

	cpuGlobal /= float64(count)
	ramGlobal /= float64(count)
	diskGlobal /= float64(count)
	globalHealth := healthFromValues(cpuGlobal, ramGlobal, diskGlobal)
	insight := dailyInsight(cpuGlobal, ramGlobal, diskGlobal, globalHealth, busiest)

	return fmt.Sprintf(`╭━━━━━━━━━━━━━━━━━━━━━━╮
┃ 🌙 DAILY SYSTEM REPORT
┃ MARZWATCH • 00:00
╰━━━━━━━━━━━━━━━━━━━━━━╯

📅 %s
🛰 24H INFRASTRUCTURE ANALYSIS

━━━━━━━━━━━━━━━━━━━━━━
💠 NETWORK OVERVIEW

🛰 ANALYZED     %d
🟢 ONLINE NOW   %d
📦 TOTAL DATA   %s
🛡 HEALTH       %s %d/100

━━━━━━━━━━━━━━━━━━━━━━
⚡ CPU • 24H

AVERAGE        %s  %s  %.1f%%
▼ LOWEST AVG   %s • %.1f%%
▲ HIGHEST AVG  %s • %.1f%%
🔥 PEAK         %s • %.1f%%

━━━━━━━━━━━━━━━━━━━━━━
🧠 MEMORY • 24H

AVERAGE        %s  %s  %.1f%%
▼ LOWEST AVG   %s • %.1f%%
▲ HIGHEST AVG  %s • %.1f%%
🔥 PEAK         %s • %.1f%%

━━━━━━━━━━━━━━━━━━━━━━
💽 STORAGE • 24H

AVERAGE        %s  %s  %.1f%%
▼ LOWEST AVG   %s • %.1f%%
▲ HIGHEST AVG  %s • %.1f%%

━━━━━━━━━━━━━━━━━━━━━━
📡 TOTAL NETWORK TRAFFIC

⬇️ DOWNLOAD    %s
⬆️ UPLOAD      %s
↕️ TOTAL       %s

━━━━━━━━━━━━━━━━━━━━━━
⚡ MAX BANDWIDTH

%s
🔥 PEAK RATE    %s

━━━━━━━━━━━━━━━━━━━━━━
🏆 BEST PERFORMANCE
%s

🔥 HIGHEST PRESSURE
%s

━━━━━━━━━━━━━━━━━━━━━━
🧠 SMART INSIGHT
%s

━━━━━━━━━━━━━━━━━━━━━━
💚 GLOBAL HEALTH
%s  %s  %d/100

╰━━━━━━━━━━━━━━━━━━━━━━╯
💠 MARZWATCH • END OF DAY`,
		date, count, onlineAtEnd, bytes(totalRX+totalTX), healthEmoji(globalHealth), globalHealth,
		statusEmoji(cpuGlobal), hudBar(cpuGlobal), cpuGlobal, empty(cpuLowNode), cpuLow, empty(cpuHighNode), cpuHigh, empty(cpuPeakNode), cpuPeak,
		statusEmoji(ramGlobal), hudBar(ramGlobal), ramGlobal, empty(ramLowNode), ramLow, empty(ramHighNode), ramHigh, empty(ramPeakNode), ramPeak,
		statusEmoji(diskGlobal), hudBar(diskGlobal), diskGlobal, empty(diskLowNode), diskLow, empty(diskHighNode), diskHigh,
		bytes(totalRX), bytes(totalTX), bytes(totalRX+totalTX), empty(peakRateNode), rate(peakRate),
		empty(lightest), empty(busiest), insight, healthEmoji(globalHealth), hudBar(float64(globalHealth)), globalHealth)
}

func formatNewNode(n model.Node) string {
	return fmt.Sprintf(`╭━━━━━━━━━━━━━━━━━━━━━━╮
┃ 🛰 NEW NODE CONNECTED
┃ MARZWATCH CORE
╰━━━━━━━━━━━━━━━━━━━━━━╯

%s %s
🌐 %s
📍 %s
🖥 %d Cores • %s • %s

🟢 STATUS • CONNECTED
🛡 Secure monitoring channel established

💚 MONITORING ACTIVE`, flagEmoji(n.Location.CountryCode), n.Name, displayIP(n.Location.PublicIP), displayPlace(n.Location), n.Cores, n.OS, n.Arch)
}

func formatAlert(n model.Node, label string, v float64, level int) string {
	icon := "🟠"
	word := "WARNING"
	if level == 2 {
		icon = "🔴"
		word = "CRITICAL"
	}
	cpu := n.Latest.CPUPercent
	ram := pct(n.Latest.MemUsed, n.Latest.MemTotal)
	disk := pct(n.Latest.DiskUsed, n.Latest.DiskTotal)
	return fmt.Sprintf(`╭━━━━━━━━━━━━━━━━━━━━━━╮
┃ %s MARZWATCH ALERT
┃ %s • %s
╰━━━━━━━━━━━━━━━━━━━━━━╯

%s %s
🌐 %s
📍 %s

%s %s  %.1f%%

━━━━━━━━━━━━━━━━━━━━━━
⚡ LIVE PRESSURE

CPU   %s  %s  %.1f%%
RAM   %s  %s  %.1f%%
DISK  %s  %s  %.1f%%

📡 NETWORK
⬇️ %s
⬆️ %s

━━━━━━━━━━━━━━━━━━━━━━
🧠 SMART ANALYSIS
فشار %s پس از چند نمونه متوالی تأیید شده است؛ این هشدار ناشی از یک Spike لحظه‌ای نیست.

🛡 HEALTH  %s %d/100
⚠️ MONITORING CONTINUES`, icon, word, label,
		flagEmoji(n.Location.CountryCode), n.Name, displayIP(n.Location.PublicIP), displayPlace(n.Location), icon, label, v,
		statusEmoji(cpu), hudBar(cpu), cpu, statusEmoji(ram), hudBar(ram), ram, statusEmoji(disk), hudBar(disk), disk,
		rate(n.Latest.RXBps), rate(n.Latest.TXBps), label, healthEmoji(health(n, true)), health(n, true))
}

func formatRecovery(n model.Node, reason string) string {
	cpu := n.Latest.CPUPercent
	ram := pct(n.Latest.MemUsed, n.Latest.MemTotal)
	disk := pct(n.Latest.DiskUsed, n.Latest.DiskTotal)
	return fmt.Sprintf(`╭━━━━━━━━━━━━━━━━━━━━━━╮
┃ ✨ SYSTEM RECOVERY
┃ MARZWATCH CORE
╰━━━━━━━━━━━━━━━━━━━━━━╯

🟢 %s

%s %s
🌐 %s
📍 %s

━━━━━━━━━━━━━━━━━━━━━━
⚡ CURRENT STATUS

CPU   %s  %s  %.1f%%
RAM   %s  %s  %.1f%%
DISK  %s  %s  %.1f%%

🛡 HEALTH  %s %d/100

💚 STABLE • RECOVERED
✅ MONITORING ACTIVE`, reason, flagEmoji(n.Location.CountryCode), n.Name, displayIP(n.Location.PublicIP), displayPlace(n.Location),
		statusEmoji(cpu), hudBar(cpu), cpu, statusEmoji(ram), hudBar(ram), ram, statusEmoji(disk), hudBar(disk), disk,
		healthEmoji(health(n, true)), health(n, true))
}

func formatOffline(n model.Node, d time.Duration) string {
	return fmt.Sprintf(`╭━━━━━━━━━━━━━━━━━━━━━━╮
┃ 🔴 NODE OFFLINE
┃ MARZWATCH ALERT
╰━━━━━━━━━━━━━━━━━━━━━━╯

%s %s
🌐 %s
📍 %s

🔴 STATUS • OFFLINE
⏱ LAST CONTACT   %s ago

━━━━━━━━━━━━━━━━━━━━━━
⚠️ Central بیش از 150 ثانیه از این Node داده‌ای دریافت نکرده است.

🛡 سایر Nodeها بدون تأثیر به مانیتورینگ ادامه می‌دهند.`, flagEmoji(n.Location.CountryCode), n.Name, displayIP(n.Location.PublicIP), displayPlace(n.Location), duration(d))
}

func hudBar(v float64) string {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	filled := int(math.Round(v / 10))
	if filled < 0 {
		filled = 0
	}
	if filled > 10 {
		filled = 10
	}
	return strings.Repeat("▰", filled) + strings.Repeat("▱", 10-filled)
}

func healthEmoji(score int) string {
	if score < 60 {
		return "🔴"
	}
	if score < 75 {
		return "🟠"
	}
	if score < 90 {
		return "🟡"
	}
	return "🟢"
}

func healthFromValues(cpu, ram, disk float64) int {
	worst := math.Max(cpu, math.Max(ram, disk))
	score := 100
	if worst > 70 {
		score -= int((worst - 70) * 1.5)
	}
	if score < 1 {
		score = 1
	}
	if score > 100 {
		score = 100
	}
	return score
}

func dailyInsight(cpu, ram, disk float64, healthScore int, busiest string) string {
	worst := math.Max(cpu, math.Max(ram, disk))
	switch {
	case healthScore >= 90 && worst < 70:
		return "🟢 زیرساخت در 24 ساعت گذشته پایدار بوده و فشار غیرعادی مداومی مشاهده نشده است."
	case healthScore >= 75:
		return fmt.Sprintf("🟡 زیرساخت پایدار است، اما %s بیشترین فشار نسبی را داشته و بهتر است روند آن زیر نظر بماند.", empty(busiest))
	default:
		return fmt.Sprintf("🟠 فشار قابل‌توجهی در بخشی از زیرساخت ثبت شده است؛ %s در صدر فشار روز قرار داشته و نیازمند بررسی دقیق‌تر است.", empty(busiest))
	}
}

'''
s = s[:start] + new + s[end:]
p.write_text(s)
