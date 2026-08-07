package central

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/collector"
	"github.com/SevinEW/marzban-monitoring/internal/config"
	"github.com/SevinEW/marzban-monitoring/internal/geo"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/security"
	"github.com/SevinEW/marzban-monitoring/internal/storage"
	"github.com/SevinEW/marzban-monitoring/internal/telegram"
)

const statePath = "/var/lib/marzwatch/state.json"
const certPath = "/var/lib/marzwatch/tls/server.crt"
const keyPath = "/var/lib/marzwatch/tls/server.key"

type alertTracker struct {
	CPU, RAM, Disk metricAlert
	Offline        bool
}
type metricAlert struct {
	Level                                  int
	HighCount, CriticalCount, RecoverCount int
}

type Server struct {
	cfg      config.Config
	store    *storage.Store
	bot      *telegram.Bot
	tz       *time.Location
	alertsMu sync.Mutex
	alerts   map[string]*alertTracker
	stop     chan struct{}
}

func Run(cfg config.Config) error {
	tz, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return fmt.Errorf("timezone: %w", err)
	}
	st, err := storage.Open(statePath)
	if err != nil {
		return err
	}
	s := &Server{cfg: cfg, store: st, bot: telegram.New(cfg.TelegramToken, cfg.AdminChatID), tz: tz, alerts: map[string]*alertTracker{}, stop: make(chan struct{})}
	fp, err := security.EnsureSelfSigned(certPath, keyPath, cfg.PublicIP)
	if err != nil {
		return err
	}
	log.Printf("central TLS fingerprint: %s", fp)
	go st.FlushLoop(s.stop, log.Printf)
	go s.supervise("local-collector", s.localCollector)
	go s.supervise("offline-loop", s.offlineLoop)
	go s.supervise("report-loop", s.reportLoop)
	go s.supervise("daily-loop", s.dailyLoop)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /api/v1/register", s.register)
	mux.HandleFunc("POST /api/v1/metrics", s.metrics)
	h := &http.Server{Addr: cfg.Listen, Handler: recoverMW(concurrencyLimit(limitBody(mux, 64<<10), 32)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	log.Printf("MarzWatch central listening on %s", cfg.Listen)
	return h.ListenAndServeTLS(certPath, keyPath)
}

func (s *Server) supervise(name string, fn func()) {
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		panicked := false
		func() {
			defer func() {
				if x := recover(); x != nil {
					panicked = true
					log.Printf("background worker %s panic recovered: %v", name, x)
				}
			}()
			fn()
		}()
		select {
		case <-s.stop:
			return
		case <-time.After(5 * time.Second):
			if !panicked {
				log.Printf("background worker %s returned; restarting", name)
			}
		}
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Marzwatch-Join")), []byte(s.cfg.JoinToken)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var q model.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	if q.Hostname == "" {
		http.Error(w, "hostname required", 400)
		return
	}
	id, _ := config.RandomToken(12)
	secret, _ := config.RandomToken(32)
	name := strings.TrimSpace(q.Name)
	if name == "" {
		name = s.autoNodeName(q.Location, q.Hostname)
	}
	n := model.Node{ID: id, Secret: secret, Name: name, Hostname: q.Hostname, OS: q.OS, Arch: q.Arch, Cores: q.Cores, Location: q.Location, Registered: time.Now(), LastSeen: time.Now(), Daily: map[string]model.DailyStats{}}
	s.store.UpsertNode(n)
	_ = s.store.Flush()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(model.RegisterResponse{NodeID: id, NodeSecret: secret})
	_ = s.bot.Send(formatNewNode(n))
}
func (s *Server) autoNodeName(loc model.Location, hostname string) string {
	base := strings.TrimSpace(loc.Country)
	if base == "" {
		base = strings.TrimSpace(loc.City)
	}
	if base == "" {
		base = strings.TrimSpace(hostname)
	}
	if base == "" {
		base = "Node"
	}
	count := 0
	for _, n := range s.store.Nodes() {
		if strings.HasPrefix(strings.ToLower(n.Name), strings.ToLower(base)+"-") {
			count++
		}
	}
	return fmt.Sprintf("%s-%02d", base, count+1)
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad body", 400)
		return
	}
	id := r.Header.Get("X-MW-Node")
	n, ok := s.store.GetNode(id)
	if !ok {
		http.Error(w, "unknown node", 401)
		return
	}
	ts := r.Header.Get("X-MW-Time")
	sig := r.Header.Get("X-MW-Signature")
	tUnix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || math.Abs(float64(time.Now().Unix()-tUnix)) > 300 {
		http.Error(w, "stale request", 401)
		return
	}
	mac := hmac.New(sha256.New, []byte(n.Secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("\n"))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(sig)) {
		http.Error(w, "bad signature", 401)
		return
	}
	var m model.Metric
	if err := json.Unmarshal(body, &m); err != nil {
		http.Error(w, "bad metric", 400)
		return
	}
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now()
	}
	updated, _ := s.store.UpdateMetric(id, m, s.tz)
	s.evaluate(updated)
	w.WriteHeader(204)
}

func (s *Server) localCollector() {
	host, osName, arch, cores := collector.SystemInfo()
	loc := geo.Detect()
	n := model.Node{ID: "central", Secret: "local", Name: s.cfg.Name, Hostname: host, OS: osName, Arch: arch, Cores: cores, Location: loc, Registered: time.Now(), Daily: map[string]model.DailyStats{}}
	if n.Name == "" {
		n.Name = "Central"
	}
	if old, ok := s.store.GetNode("central"); ok {
		n.Registered = old.Registered
		n.Daily = old.Daily
		n.Latest = old.Latest
	}
	s.store.UpsertNode(n)
	c := collector.New()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	lastSend := time.Time{}
	for {
		select {
		case <-ticker.C:
			m, err := c.Collect()
			if err != nil {
				log.Printf("local collect partial error: %v", err)
			}
			if lastSend.IsZero() || time.Since(lastSend) >= 55*time.Second {
				updated, _ := s.store.UpdateMetric("central", m, s.tz)
				s.evaluate(updated)
				lastSend = time.Now()
			}
		case <-s.stop:
			return
		}
	}
}

func (s *Server) evaluate(n model.Node) {
	s.alertsMu.Lock()
	tr := s.alerts[n.ID]
	if tr == nil {
		tr = &alertTracker{}
		s.alerts[n.ID] = tr
	}
	wasOffline := tr.Offline
	if tr.Offline {
		tr.Offline = false
	}
	s.alertsMu.Unlock()
	if wasOffline {
		_ = s.bot.Send(formatRecovery(n, "ارتباط سرور دوباره برقرار شد"))
	}
	cpu := n.Latest.CPUPercent
	ram := pct(n.Latest.MemUsed, n.Latest.MemTotal)
	disk := pct(n.Latest.DiskUsed, n.Latest.DiskTotal)
	s.evalMetric(n, "CPU", cpu, 90, 97, 80, 5, 3, &tr.CPU)
	s.evalMetric(n, "RAM", ram, 85, 95, 80, 5, 3, &tr.RAM)
	s.evalMetric(n, "DISK", disk, 85, 95, 80, 1, 1, &tr.Disk)
}
func (s *Server) evalMetric(n model.Node, label string, v, warn, crit, recover float64, warnN, critN int, a *metricAlert) {
	s.alertsMu.Lock()
	defer s.alertsMu.Unlock()
	prev := a.Level
	if v >= crit {
		a.CriticalCount++
		a.HighCount++
	} else if v >= warn {
		a.HighCount++
		a.CriticalCount = 0
	} else {
		a.HighCount = 0
		a.CriticalCount = 0
	}
	if v < recover {
		a.RecoverCount++
	} else {
		a.RecoverCount = 0
	}
	if a.Level < 2 && a.CriticalCount >= critN {
		a.Level = 2
	} else if a.Level == 0 && a.HighCount >= warnN {
		a.Level = 1
	} else if a.Level > 0 && a.RecoverCount >= 3 {
		a.Level = 0
	}
	if a.Level != prev {
		go func(level int) {
			var msg string
			if level == 0 {
				msg = formatRecovery(n, fmt.Sprintf("%s به محدوده پایدار برگشت", label))
			} else {
				msg = formatAlert(n, label, v, level)
			}
			if err := s.bot.Send(msg); err != nil {
				log.Printf("telegram alert: %v", err)
			}
		}(a.Level)
	}
}

func (s *Server) offlineLoop() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			now := time.Now()
			for _, n := range s.store.Nodes() {
				if n.LastSeen.IsZero() {
					continue
				}
				off := now.Sub(n.LastSeen) > 150*time.Second
				s.alertsMu.Lock()
				tr := s.alerts[n.ID]
				if tr == nil {
					tr = &alertTracker{}
					s.alerts[n.ID] = tr
				}
				changed := off && !tr.Offline
				if off {
					tr.Offline = true
				}
				s.alertsMu.Unlock()
				if changed {
					_ = s.bot.Send(formatOffline(n, now.Sub(n.LastSeen)))
				}
			}
		case <-s.stop:
			return
		}
	}
}
func (s *Server) reportLoop() {
	for {
		next := nextBoundary(time.Now(), 10*time.Minute)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-timer.C:
			if err := s.bot.Send(formatGlobal(s.store.Nodes(), time.Now().In(s.tz))); err != nil {
				log.Printf("telegram report: %v", err)
			}
		case <-s.stop:
			timer.Stop()
			return
		}
	}
}
func (s *Server) dailyLoop() {
	for {
		now := time.Now().In(s.tz)
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, s.tz)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-timer.C:
			date := next.Add(-time.Second).Format("2006-01-02")
			if err := s.bot.Send(formatDaily(s.store.Nodes(), date)); err != nil {
				log.Printf("telegram daily: %v", err)
			}
		case <-s.stop:
			timer.Stop()
			return
		}
	}
}
func nextBoundary(t time.Time, d time.Duration) time.Time { return t.Truncate(d).Add(d) }

func formatGlobal(nodes []model.Node, now time.Time) string {
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
┃ 💠  MARZWATCH CORE
┃ GLOBAL INFRASTRUCTURE
╰━━━━━━━━━━━━━━━━━━━━━━╯

🟢 LIVE • %s

🛰 NODES       %d
🟢 STABLE      %d
🟡 WARNING     %d
🔴 OFFLINE     %d

━━━━━━━━━━━━━━━━━━━━━━
⚡ GLOBAL LOAD
CPU AVG    %s %.1f%%
🧠 RAM AVG    %s %.1f%%
💽 DISK AVG   %s %.1f%%
🛡 HEALTH     %d/100

━━━━━━━━━━━━━━━━━━━━━━
📡 LIVE BANDWIDTH
⬇️ DOWNLOAD   %s
⬆️ UPLOAD     %s
↕️ TOTAL      %s

📦 TRAFFIC TODAY
⬇️ DOWNLOAD   %s
⬆️ UPLOAD     %s
↕️ TOTAL      %s

━━━━━━━━━━━━━━━━━━━━━━
`, now.Format("15:04:05"), len(nodes), stable, warning, offline,
		statusEmoji(cpu), cpu, statusEmoji(ram), ram, statusEmoji(disk), disk, globalHealth,
		rate(rx), rate(tx), rate(rx+tx), bytes(todayRX), bytes(todayTX), bytes(todayRX+todayTX))
	return head + strings.Join(blocks, "\n") + "\n💚 MONITORING ACTIVE"
}

func formatNodeBlock(n model.Node, on bool, date string) string {
	flag := flagEmoji(n.Location.CountryCode)
	state := "🟢 STABLE"
	worst := math.Max(n.Latest.CPUPercent, math.Max(pct(n.Latest.MemUsed, n.Latest.MemTotal), pct(n.Latest.DiskUsed, n.Latest.DiskTotal)))
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

⚡ CPU
%.1f%% • %d Cores • Load %.2f

🧠 RAM
%s / %s • %.1f%%

💽 STORAGE
%s / %s • %.1f%%

📡 BANDWIDTH NOW
⬇️ %s
⬆️ %s
↕️ %s

📦 TRAFFIC TODAY
⬇️ %s
⬆️ %s
↕️ %s

⏱ UPTIME  %s
🛡 HEALTH  %d/100
┗━━━━━━━━━━━━━━━━━━━━━━┛
`, flag, n.Name, displayIP(n.Location.PublicIP), displayPlace(n.Location), state,
		n.Latest.CPUPercent, n.Cores, n.Latest.Load1,
		bytes(n.Latest.MemUsed), bytes(n.Latest.MemTotal), pct(n.Latest.MemUsed, n.Latest.MemTotal),
		bytes(n.Latest.DiskUsed), bytes(n.Latest.DiskTotal), pct(n.Latest.DiskUsed, n.Latest.DiskTotal),
		rate(n.Latest.RXBps), rate(n.Latest.TXBps), rate(n.Latest.RXBps+n.Latest.TXBps),
		bytes(todayRX), bytes(todayTX), bytes(todayRX+todayTX), durationLong(time.Duration(n.Latest.UptimeSeconds)*time.Second), health(n, on))
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
	if count > 0 {
		cpuGlobal /= float64(count)
		ramGlobal /= float64(count)
		diskGlobal /= float64(count)
	}
	if count == 0 {
		return fmt.Sprintf("🌙 MarzWatch Daily Report • %s\n\nهنوز داده کافی برای گزارش روزانه ثبت نشده است.", date)
	}

	return fmt.Sprintf(`╭━━━━━━━━━━━━━━━━━━━━━━╮
┃ 🌙 DAILY SYSTEM REPORT
┃ MARZWATCH • 00:00
╰━━━━━━━━━━━━━━━━━━━━━━╯

📅 %s
🛰 24H INFRASTRUCTURE ANALYSIS

━━━━━━━━━━━━━━━━━━━━━━
⚡ CPU • 24H

AVERAGE
%s %.1f%%

▼ LOWEST AVG
%s • %.1f%%

▲ HIGHEST AVG
%s • %.1f%%

🔥 PEAK
%s • %.1f%%

━━━━━━━━━━━━━━━━━━━━━━
🧠 MEMORY • 24H

AVERAGE
%s %.1f%%

▼ LOWEST AVG
%s • %.1f%%

▲ HIGHEST AVG
%s • %.1f%%

🔥 PEAK
%s • %.1f%%

━━━━━━━━━━━━━━━━━━━━━━
💽 STORAGE • 24H
AVERAGE     %.1f%%
▼ LOWEST    %s • %.1f%%
▲ HIGHEST   %s • %.1f%%

━━━━━━━━━━━━━━━━━━━━━━
📡 TOTAL NETWORK TRAFFIC
⬇️ DOWNLOAD  %s
⬆️ UPLOAD    %s
↕️ TOTAL     %s

⚡ HIGHEST NODE PEAK
%s
%s

━━━━━━━━━━━━━━━━━━━━━━
🏆 BEST PERFORMANCE
%s

🔥 HIGHEST PRESSURE
%s

━━━━━━━━━━━━━━━━━━━━━━
💚 END OF DAY • MONITORING ACTIVE`,
		date,
		statusEmoji(cpuGlobal), cpuGlobal,
		empty(cpuLowNode), cpuLow,
		empty(cpuHighNode), cpuHigh,
		empty(cpuPeakNode), cpuPeak,
		statusEmoji(ramGlobal), ramGlobal,
		empty(ramLowNode), ramLow,
		empty(ramHighNode), ramHigh,
		empty(ramPeakNode), ramPeak,
		diskGlobal, empty(diskLowNode), diskLow, empty(diskHighNode), diskHigh,
		bytes(totalRX), bytes(totalTX), bytes(totalRX+totalTX),
		empty(peakRateNode), rate(peakRate), empty(lightest), empty(busiest))
}

func formatNewNode(n model.Node) string {
	return fmt.Sprintf(`━━━━━━━━━━━━━━━━━━━━━━
🆕 NEW NODE CONNECTED
━━━━━━━━━━━━━━━━━━━━━━

%s %s
🌐 %s
📍 %s
🖥 %d Cores • %s
🟢 وضعیت: متصل و آماده مانیتورینگ

🛡 اتصال امن برقرار شد.`, flagEmoji(n.Location.CountryCode), n.Name, displayIP(n.Location.PublicIP), displayPlace(n.Location), n.Cores, n.OS)
}

func formatAlert(n model.Node, label string, v float64, level int) string {
	icon := "🟠"
	word := "HIGH"
	if level == 2 {
		icon = "🔴"
		word = "CRITICAL"
	}
	return fmt.Sprintf(`━━━━━━━━━━━━━━━━━━━━━━
%s SYSTEM %s
━━━━━━━━━━━━━━━━━━━━━━

%s فشار غیرعادی روی %s شناسایی شد

%s %s
🌐 %s
📍 %s

%s %s  %.1f%%
⚡ CPU   %.1f%%
🧠 RAM   %.1f%%
💽 DISK  %.1f%%
⬇️ %s
⬆️ %s

🧠 SMART ANALYSIS
مصرف این منبع به‌صورت پایدار از محدوده امن عبور کرده است؛ این پیام بعد از تأیید چند نمونه متوالی ارسال شده، نه یک Spike لحظه‌ای.

🛡 HEALTH %d/100`, icon, word, icon, label, flagEmoji(n.Location.CountryCode), n.Name, displayIP(n.Location.PublicIP), displayPlace(n.Location), icon, label, v, n.Latest.CPUPercent, pct(n.Latest.MemUsed, n.Latest.MemTotal), pct(n.Latest.DiskUsed, n.Latest.DiskTotal), rate(n.Latest.RXBps), rate(n.Latest.TXBps), health(n, true))
}

func formatRecovery(n model.Node, reason string) string {
	return fmt.Sprintf(`━━━━━━━━━━━━━━━━━━━━━━
✨ SYSTEM RECOVERY
━━━━━━━━━━━━━━━━━━━━━━

🟢 %s

%s %s
🌐 %s

⚡ CPU   %.1f%%
🧠 RAM   %.1f%%
💽 DISK  %.1f%%

💚 وضعیت فعلی: STABLE
✅ RECOVERED • MONITORING ACTIVE`, reason, flagEmoji(n.Location.CountryCode), n.Name, displayIP(n.Location.PublicIP), n.Latest.CPUPercent, pct(n.Latest.MemUsed, n.Latest.MemTotal), pct(n.Latest.DiskUsed, n.Latest.DiskTotal))
}

func formatOffline(n model.Node, d time.Duration) string {
	return fmt.Sprintf(`━━━━━━━━━━━━━━━━━━━━━━
🔴 NODE OFFLINE
━━━━━━━━━━━━━━━━━━━━━━

%s %s
🌐 %s
📍 %s

⏱ آخرین ارتباط: %s قبل
⚠️ Central بیش از 150 ثانیه از این نود داده‌ای دریافت نکرده است.

🛡 سایر نودها بدون تأثیر ادامه می‌دهند.`, flagEmoji(n.Location.CountryCode), n.Name, displayIP(n.Location.PublicIP), displayPlace(n.Location), duration(d))
}

func pct(a, b uint64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) * 100 / float64(b)
}
func statusEmoji(v float64) string {
	if v >= 95 {
		return "🔴"
	}
	if v >= 85 {
		return "🟠"
	}
	if v >= 70 {
		return "🟡"
	}
	return "🟢"
}
func health(n model.Node, on bool) int {
	if !on {
		return 0
	}
	worst := math.Max(n.Latest.CPUPercent, math.Max(pct(n.Latest.MemUsed, n.Latest.MemTotal), pct(n.Latest.DiskUsed, n.Latest.DiskTotal)))
	score := 100
	if worst > 70 {
		score -= int((worst - 70) * 1.5)
	}
	if score < 1 {
		score = 1
	}
	return score
}
func rate(v float64) string {
	units := []string{"bps", "Kbps", "Mbps", "Gbps", "Tbps"}
	i := 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	if i < 2 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.2f %s", v, units[i])
}
func bytes(v uint64) string {
	x := float64(v)
	u := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0
	for x >= 1000 && i < len(u)-1 {
		x /= 1000
		i++
	}
	return fmt.Sprintf("%.2f %s", x, u[i])
}
func displayIP(s string) string {
	if s == "" {
		return "Unknown"
	}
	return s
}
func displayPlace(l model.Location) string {
	p := []string{}
	if l.City != "" {
		p = append(p, l.City)
	}
	if l.Country != "" {
		p = append(p, l.Country)
	}
	if len(p) == 0 {
		return "Unknown"
	}
	return strings.Join(p, ", ")
}
func empty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
func duration(d time.Duration) string {
	d = d.Round(time.Second)
	m := int(d.Minutes())
	return fmt.Sprintf("%dm %02ds", m, int(d.Seconds())%60)
}
func durationLong(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	mins := int(d / time.Minute)
	if days > 0 {
		return fmt.Sprintf("%dd %02dh %02dm", days, hours, mins)
	}
	return fmt.Sprintf("%dh %02dm", hours, mins)
}

func flagEmoji(cc string) string {
	cc = strings.ToUpper(cc)
	if len(cc) != 2 {
		return "🌐"
	}
	r := []rune(cc)
	return string([]rune{r[0] - 'A' + 0x1F1E6, r[1] - 'A' + 0x1F1E6})
}

func limitBody(next http.Handler, max int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, max)
		next.ServeHTTP(w, r)
	})
}
func concurrencyLimit(next http.Handler, max int) http.Handler {
	sem := make(chan struct{}, max)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "busy", http.StatusServiceUnavailable)
		}
	})
}

func recoverMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if x := recover(); x != nil {
				log.Printf("http panic recovered: %v", x)
				http.Error(w, "internal error", 500)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
