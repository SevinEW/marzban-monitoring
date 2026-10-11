"""Opt-in independent PasarGuard rolling traffic; retain existing HUD layout."""
from pathlib import Path
def edit(path,old,new):
 p=Path(path);s=p.read_text(encoding='utf-8')
 if s.count(old)!=1:raise SystemExit('PG traffic anchor missing: '+old)
 p.write_text(s.replace(old,new,1),encoding='utf-8')
edit('internal/config/config.go','type Config struct {','type Config struct {\n PasarGuardTraffic bool `json:"pasarguard_traffic,omitempty"`\n PasarGuardNodes map[string]string `json:"pasarguard_nodes,omitempty"`')
edit('internal/central/forum.go','return formatOverviewLive(s.store.Nodes(), time.Now().In(s.tz))','return s.formatPGOverview(s.store.Nodes(), time.Now().In(s.tz))')
edit('internal/central/forum.go','appendNodeLastUpdate(formatNodeBlock(n, on, time.Now().In(s.tz).Format("2006-01-02"))','appendNodeLastUpdate(s.formatPGNode(n, on, time.Now().In(s.tz).Format("2006-01-02"))')
