"""Use Iran civil dates for accounting and all live/report render paths."""
from pathlib import Path

def once(s, old, new):
    if s.count(old) != 1:
        raise SystemExit(f'Iran day anchor missing/ambiguous: {old[:100]!r}')
    return s.replace(old, new, 1)

p=Path('internal/central/server.go')
s=p.read_text(encoding='utf-8')
s=once(s,'import (','import (\n "github.com/SevinEW/marzban-monitoring/internal/reportclock"')
s=once(s,'\ttz, err := time.LoadLocation(cfg.Timezone)\n\tif err != nil {\n\t\treturn fmt.Errorf("timezone: %w", err)\n\t}', '\ttz := reportclock.Iran\n\tcfg.Timezone = reportclock.Zone')
p.write_text(s,encoding='utf-8')

p=Path('cmd/marzwatch/main.go')
s=p.read_text(encoding='utf-8')
s=once(s,'tz := ask(r, "Timezone report", "Asia/Tehran")','tz := "Asia/Tehran"\n\tfmt.Println("Daily accounting timezone: Asia/Tehran (00:00 reset)")')
p.write_text(s,encoding='utf-8')

p=Path('internal/storage/store.go')
s=p.read_text(encoding='utf-8')
s=once(s,'import (','import (\n "github.com/SevinEW/marzban-monitoring/internal/reportclock"')
s=once(s,'date := m.Timestamp.In(loc).Format("2006-01-02")','date := reportclock.Date(m.Timestamp)')
start=s.index('\tif !prev.Timestamp.IsZero() {',s.index('func (s *Store) UpdateMetric'))
end=s.index('\tif m.RXBps > ds.PeakRXBps {',start)
s=s[:start]+s[end:]
s=once(s,'\tn.Daily[date] = ds\n\tpruneDaily', '\tn.Daily[date] = ds\n\taddDailyTraffic(n, prev, m)\n\tpruneDaily')
p.write_text(s,encoding='utf-8')
