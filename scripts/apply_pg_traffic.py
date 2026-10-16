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

# Pace edits independently from topic creation while retaining one shared flood cooldown.
edit('internal/telegram/bot.go','type Bot struct {','type Bot struct {\n editSpacing time.Duration')
edit('internal/telegram/live_card.go','\t\terr := b.withGroupWrite(chatID, func() error {\n\t\t\ttext = render()', '\t\terr := b.withGroupEdit(chatID, func() error {\n\t\t\ttext = render()')
# A successful card edit also proves its topic exists. Avoid a redundant rename probe.
edit('internal/central/forum.go','st.OverviewMessageID, st.OverviewLastText, st.OverviewSendPending = card.MessageID, card.LastText, card.Pending','st.OverviewMessageID, st.OverviewLastText, st.OverviewSendPending = card.MessageID, card.LastText, card.Pending\n if card.MessageID != 0 && !card.Pending && card.LastText != "" { st.OverviewTopicChecked = time.Now() }')
edit('internal/central/forum.go','fs.MessageID, fs.LastText, fs.SendPending = card.MessageID, card.LastText, card.Pending','fs.MessageID, fs.LastText, fs.SendPending = card.MessageID, card.LastText, card.Pending\n if card.MessageID != 0 && !card.Pending && card.LastText != "" { fs.TopicChecked = time.Now() }')

# Refresh the summary between bounded groups of nodes without starving any node.
p=Path('internal/central/forum.go');s=p.read_text(encoding='utf-8')
start=s.index('\n topic:=telegram.LiveTopic{ID:st.OverviewTopicID')
end=s.index('\n\tseen := map[string]bool{}',start)
block=s[start:end]
block=block.replace('\tif changed {\n\t\tif err := saveForumState(*st); err != nil { return err }\n\t}\n','')
s=s[:start]+'\n if err:=s.syncOverview(st);err!=nil{return err}\n'+s[end:]
s+='\nfunc (s *Server) syncOverview(st *forumState) error {\n'+block+'\n return nil\n}\n'
s=s.replace('\tfor _, n := range nodes {','\tfor index, n := range nodes {',1)
anchor='\n\t\tst.Nodes[n.ID] = fs\n\t}\n'
assert s.count(anchor)==1,'node loop anchor missing'
s=s.replace(anchor,'\n\t\tst.Nodes[n.ID] = fs\n if (index+1)%3==0 && index+1<len(nodes) { if err:=s.syncOverview(st);err!=nil{log.Printf("overview refresh: %v",err)} }\n\t}\n',1)
s=s.replace('time.NewTicker(5 * time.Second)','time.NewTicker(2 * time.Second)',1)
p.write_text(s,encoding='utf-8')
