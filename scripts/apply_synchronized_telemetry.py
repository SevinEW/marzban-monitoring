"""Final transform: shared five-second sampling and just-in-time forum cards."""
from pathlib import Path

def once(s, old, new):
    if s.count(old) != 1:
        raise SystemExit(f'Expected one synchronization anchor: {old[:120]!r}')
    return s.replace(old, new, 1)

def read(name):
    return Path(name).read_text(encoding='utf-8')

def write(name, text):
    Path(name).write_text(text, encoding='utf-8')

name = 'internal/agent/agent.go'
s = read(name)
s = once(s, '\t"sync"\n', '')
s = once(s, '\t"github.com/SevinEW/marzban-monitoring/internal/collector"', '\t"github.com/SevinEW/marzban-monitoring/internal/cadence"\n\t"github.com/SevinEW/marzban-monitoring/internal/collector"')
s = once(s, '\tmu     sync.RWMutex\n\tlatest model.Metric', '\tclock cadence.Clock\n\tsamples chan model.Metric')
s = once(s, '\tgo a.superviseCollector()', '\ta.samples = make(chan model.Metric, 1)\n\tgo a.superviseCollector()')
start = s.index('func (a *Agent) collectLoop() {')
end = s.index('func (a *Agent) sendMetric(', start)
s = s[:start] + '''func (a *Agent) collectLoop() {
	// Prime CPU/network counters and allow an existing identity to learn Central's
	// clock immediately. All subsequent samples use shared five-second slots.
	m, _ := a.col.Collect()
	m.Timestamp = a.clock.Now()
	offerLatest(a.samples, m)
	for {
		time.Sleep(a.clock.Delay())
		m, err := a.col.Collect()
		if err != nil { log.Printf("collect partial error: %v", err) }
		m.Timestamp = a.clock.Now()
		offerLatest(a.samples, m)
	}
}

func (a *Agent) sendLoop() error {
	delay := cadence.Interval
	for {
		m := <-a.samples
		if err := a.sendMetric(m); err != nil {
			if errors.Is(err, errIdentityRejected) {
				log.Printf("node identity rejected; starting safe automatic re-registration")
				if err := a.resetIdentity(); err != nil {
					log.Printf("identity reset failed: %v", err)
				} else if err := a.registerWithRetry(); err != nil {
					log.Printf("automatic re-registration failed: %v", err)
				} else {
					log.Printf("automatic re-registration completed")
					delay = cadence.Interval
					continue
				}
			}
			log.Printf("metrics send failed: %v", err)
			time.Sleep(delay)
			delay *= 2
			if delay > time.Minute { delay = time.Minute }
		} else {
			delay = cadence.Interval
		}
	}
}

''' + s[end:]
s = once(s, 'ts := fmt.Sprintf("%d", time.Now().Unix())', 'ts := fmt.Sprintf("%d", a.clock.Now().Unix())')
# Both registration and metric responses are protected by the pinned TLS peer.
if s.count('\tresp, err := a.client.Do(req)') != 2:
    raise SystemExit('Expected two agent HTTP exchanges')
s = s.replace('\tresp, err := a.client.Do(req)', '\tstarted := time.Now()\n\tresp, err := a.client.Do(req)')
s = s.replace('\tdefer resp.Body.Close()\n', '\tdefer resp.Body.Close()\n\ta.clock.Observe(resp.Header.Get(cadence.TimeHeader), started, time.Now())\n')
write(name, s)

name = 'internal/central/server.go'
s = read(name)
s = once(s, '\t"github.com/SevinEW/marzban-monitoring/internal/collector"', '\t"github.com/SevinEW/marzban-monitoring/internal/cadence"\n\t"github.com/SevinEW/marzban-monitoring/internal/collector"')
for method in ['register', 'metrics']:
    anchor = f'func (s *Server) {method}(w http.ResponseWriter, r *http.Request) {{'
    s = once(s, anchor, anchor + '\n\tw.Header().Set(cadence.TimeHeader, time.Now().UTC().Format(time.RFC3339Nano))')
start = s.index('\tticker := ', s.index('func (s *Server) localCollector()'))
end = s.index('\nfunc (s *Server) evaluate(', start)
s = s[:start] + '''	// Prime delta counters before joining the same UTC slots as remote nodes.
	_, _ = c.Collect()
	for {
		timer := time.NewTimer(time.Until(cadence.Next(time.Now())))
		select {
		case <-timer.C:
			m, err := c.Collect()
			if err != nil { log.Printf("local collect partial error: %v", err) }
			updated, _ := s.store.UpdateMetric("central", m, s.tz)
			s.evaluate(updated)
		case <-s.stop:
			timer.Stop()
			return
		}
	}
}
''' + s[end:]
write(name, s)

name = 'internal/central/forum.go'
s = read(name)
s = once(s, 'func (s *Server) syncForum(st *forumState) error {\n\tnow := time.Now()\n', 'func (s *Server) syncForum(st *forumState) error {\n')
# Exact anchors below are deliberately checked against the complete release
# transforms; the HUD and durable recovery logic are left intact.
start = s.index('\toverview := formatOverviewLive(')
end = s.index('\n', start)
s = s[:start] + '''	overview := func() string {
		return formatOverviewLive(s.store.Nodes(), time.Now().In(s.tz))
	}''' + s[end:]
s = once(s, 's.bot.SyncLiveCard(s.cfg.TelegramGroupID, st.OverviewTopicID, overview,', 's.bot.SyncLiveCardLatest(s.cfg.TelegramGroupID, st.OverviewTopicID, overview,')
# Fresh-node rendering is completed below using the existing formatting lines.
start = s.index('\tdate := ', s.index('func (s *Server) syncForum('))
end = s.index('\n', start)
s = s[:start] + s[end+1:]
start = s.index('\t\ton := ', s.index('func (s *Server) syncForum('))
end = s.index('\n', s.index('\t\ttext := ', start))
original = s[start:end]
body = original.replace('now.Sub(', 'time.Now().Sub(').replace('date)', 'time.Now().In(s.tz).Format("2006-01-02"))')
body = body.replace('\t\ttext := ', '\t\treturn ')
s = s[:start] + '''		text := func() string {
			latest, ok := s.store.GetNode(n.ID)
			if !ok { return "" }
			n := latest
''' + body + '\n\t\t}' + s[end:]
s = once(s, 's.bot.SyncLiveCard(s.cfg.TelegramGroupID, fs.TopicID, text,', 's.bot.SyncLiveCardLatest(s.cfg.TelegramGroupID, fs.TopicID, text,')
write(name, s)
