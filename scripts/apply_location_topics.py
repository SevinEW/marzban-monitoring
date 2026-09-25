"""Apply after synchronized telemetry: metadata recovery and stable topic names."""
from pathlib import Path

def once(s, old, new):
    if s.count(old)!=1: raise SystemExit(f'Location anchor missing/ambiguous: {old[:100]!r}')
    return s.replace(old,new,1)

p=Path('internal/central/server.go')
s=p.read_text(encoding='utf-8')
s=once(s,'\tgo s.supervise("local-collector", s.localCollector)', '''	go s.supervise("location-refresh", s.locationLoop)
	if cfg.TelegramGroupID != 0 {
		go s.bot.CleanTopicRenames(cfg.TelegramGroupID, s.stop, "/var/lib/marzwatch/topic-update-offset.json", func(id int) bool {
			st := loadForumState()
			if id == st.OverviewTopicID && id != 0 { return true }
			for _, n := range st.Nodes { if n.TopicID == id && id != 0 { return true } }
			return false
		}, log.Printf)
	}
	go s.supervise("local-collector", s.localCollector)''')
s=once(s,'\tupdated, _ := s.store.UpdateMetric(id, m, s.tz)', '\ts.store.SetPublicIP(id, geo.PublicIP(m.PublicIP))\n\ts.store.FillPublicIP(id, peerPublicIP(r.RemoteAddr))\n\tupdated, _ := s.store.UpdateMetric(id, m, s.tz)')
s=once(s,'\tn := model.Node{ID: id, Secret: secret,', '\tif geo.PublicIP(q.Location.PublicIP) == "" { q.Location.PublicIP = peerPublicIP(r.RemoteAddr) }\n\tn := model.Node{ID: id, Secret: secret,')
s=once(s,'\tloc := geo.Detect()\n\tn := model.Node{ID: "central"', '\tloc := geo.Detect()\n\tif override, ok := s.cfg.LocationOverrides["central"]; ok { override.PublicIP = loc.PublicIP; loc = override }\n\tn := model.Node{ID: "central"')
p.write_text(s,encoding='utf-8')
p=Path('internal/central/forum.go')
s=p.read_text(encoding='utf-8')
s=once(s, '\tfor index, n := range nodes {', '\tfor _, n := range nodes {')
s=once(s, 'name := fmt.Sprintf("%02d · %s", index+1, forumTopicName(n))', 'name := forumTopicName(n)')
s=once(s,'return flagEmoji(n.Location.CountryCode) + " " + name', 'cc := countryKey(n)\n\tif cc == "ZZZ" { cc = "Unknown" }\n\treturn flagEmoji(n.Location.CountryCode) + " " + cc + " · " + name')
p.write_text(s,encoding='utf-8')
