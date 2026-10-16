"""Opt-in independent PasarGuard rolling traffic; retain existing HUD layout."""
from pathlib import Path
def edit(path,old,new):
 p=Path(path);s=p.read_text(encoding='utf-8')
 if s.count(old)!=1:raise SystemExit('PG traffic anchor missing: '+old)
 p.write_text(s.replace(old,new,1),encoding='utf-8')
edit('internal/config/config.go','type Config struct {','type Config struct {\n HostTraffic bool `json:"host_traffic,omitempty"`\n PasarGuardTraffic bool `json:"pasarguard_traffic,omitempty"`\n PasarGuardNodes map[string]string `json:"pasarguard_nodes,omitempty"`')
edit('internal/central/forum.go','return formatOverviewLive(s.store.Nodes(), time.Now().In(s.tz))','return s.formatPGOverview(s.store.Nodes(), time.Now().In(s.tz))')
edit('internal/central/forum.go','appendNodeLastUpdate(formatNodeBlock(n, on, time.Now().In(s.tz).Format("2006-01-02"))','appendNodeLastUpdate(s.formatPGNode(n, on, time.Now().In(s.tz).Format("2006-01-02"))')

edit('internal/central/server.go','type Server struct {','type Server struct {\n host *hostTraffic')
edit('internal/central/server.go','fp, err := security.EnsureSelfSigned','if cfg.HostTraffic {\n s.host, err = openHostTraffic(hostTrafficPath)\n if err != nil { return err }\n go s.supervise("host-traffic", s.hostTrafficLoop)\n }\n fp, err := security.EnsureSelfSigned')
