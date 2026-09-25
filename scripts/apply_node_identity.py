"""Final integration: durable identities, explicit aliases, recoverable topics."""
from pathlib import Path

def once(s, old, new):
    if s.count(old) != 1:
        raise SystemExit(f'Identity anchor missing/ambiguous: {old[:120]!r}')
    return s.replace(old, new, 1)

def edit(path, fn):
    p=Path(path)
    p.write_text(fn(p.read_text(encoding='utf-8')),encoding='utf-8')

def model(s):
    for name in ['Node','Metric','RegisterRequest']:
        s=once(s,f'type {name} struct {{',f'type {name} struct {{\n ServerKey string `json:"server_key,omitempty"`')
    return s
edit('internal/model/model.go',model)
edit('internal/config/config.go',lambda s:once(s,'type Config struct {','type Config struct {\n NodeAliases map[string]string `json:"node_aliases,omitempty"`'))
edit('cmd/marzwatch/main.go',lambda s:once(s,'\tcase "location":','\tcase "merge-nodes":\n\t\tmustRoot()\n\t\tmergeNodes()\n\tcase "location":'))

def store(s):
    s=once(s,'type diskState struct {','type diskState struct {\n Aliases map[string]string `json:"aliases,omitempty"`')
    s=once(s,'type Store struct {','type Store struct {\n aliases map[string]string')
    s=once(s,'s := &Store{path: path, nodes: map[string]*model.Node{}}','s := &Store{path: path, nodes: map[string]*model.Node{}, aliases: map[string]string{}}')
    s=once(s,'\treturn s, nil\n}', '\tif d.Aliases!=nil { if err:=s.ApplyAliases(d.Aliases);err!=nil { return nil,err } }\n\treturn s, nil\n}')
    s=once(s,'\tdelete(s.nodes, id)','''
 // Do not remove canonical identities with archived aliases: their credentials
 // and history must survive long outages and late authenticated packets.
 for from,to:=range s.aliases { if from==id || to==id { return model.Node{},false } }
 delete(s.nodes, id)''')
    s=once(s,'\tfor _, n := range s.nodes {\n\t\tout = append(out, cloneNode(*n))','\tfor id, n := range s.nodes {\n\t\tif s.aliases[id]!="" { continue }\n\t\tout = append(out, cloneNode(*n))')
    s=once(s,'\tprev := n.Latest','''
 // Keep per-identity counters separate. Late packets for an archived identity
 // are authenticated but cannot overwrite the chosen server or double-count it.
 if s.aliases[id]!="" { return cloneNode(*s.nodes[s.aliases[id]]),false }
 if !n.Latest.Timestamp.IsZero() && !m.Timestamp.After(n.Latest.Timestamp) { return cloneNode(*n),false }
 prev := n.Latest''')
    s=once(s,'\tdefer s.mu.Unlock()\n\tif !s.dirty {','\tdefer s.mu.Unlock()\n\treturn s.flushLocked()\n}\n\nfunc (s *Store) flushLocked() error {\n\tif !s.dirty {')
    s=once(s,'diskState{Nodes: s.nodes}', 'diskState{Nodes: s.nodes, Aliases:s.aliases}')
    return s
edit('internal/storage/store.go',store)

def agent(s):
    s=once(s,'type Agent struct {','type Agent struct {\n serverKey string')
    s=once(s,'\t_ = a.loadIdentity()', '\tkey,err:=loadMachineKey();if err!=nil { return err };a.serverKey=key\n\t_ = a.loadIdentity()')
    s=once(s,'q := model.RegisterRequest{','q := model.RegisterRequest{ServerKey:a.serverKey,')
    s=once(s,'func (a *Agent) sendMetric(m model.Metric) error {','func (a *Agent) sendMetric(m model.Metric) error {\n m.ServerKey=a.serverKey')
    s=once(s,'\t\treturn errIdentityRejected','''
        msg,_:=io.ReadAll(io.LimitReader(resp.Body,512))
        if strings.TrimSpace(string(msg))=="unknown node" { return errIdentityRejected }
        return fmt.Errorf("central rejected request; retaining node identity")''')
    return s
edit('internal/agent/agent.go',agent)

def server(s):
    s=once(s,'\ts := &Server{','\tif err:=st.ApplyAliases(cfg.NodeAliases);err!=nil { return fmt.Errorf("node aliases: %w",err) }\n\tif err:=st.Flush();err!=nil { return err }\n\ts := &Server{')
    s=once(s,'\tid, _ := config.RandomToken(12)\n\tsecret, _ := config.RandomToken(32)', '''
 if q.ServerKey!="" && !storage.ValidServerKey(q.ServerKey) { http.Error(w,"invalid machine identity",400);return }
 id,err:=config.RandomToken(12);if err!=nil { http.Error(w,"identity unavailable",500);return }
 secret,err:=config.RandomToken(32);if err!=nil { http.Error(w,"identity unavailable",500);return }''')
    s=once(s,'\tn := model.Node{ID: id, Secret: secret,','\tn := model.Node{ServerKey:q.ServerKey, ID: id, Secret: secret,')
    s=once(s,'\ts.store.UpsertNode(n)\n\t_ = s.store.Flush()', '\tn,err=s.store.Register(n)\n\tif err!=nil { http.Error(w,"registration persistence failed",503);return }\n\tid,secret=n.ID,n.Secret')
    s=once(s,'\ts.store.SetPublicIP(id, geo.PublicIP(m.PublicIP))','\tif err:=s.store.BindServerKey(id,m.ServerKey);err!=nil { http.Error(w,"machine identity conflict",409);return }\n\ts.store.SetPublicIP(id, geo.PublicIP(m.PublicIP))')
    s=once(s,'\tupdated, _ := s.store.UpdateMetric(id, m, s.tz)\n\ts.evaluate(updated)','\tupdated, accepted := s.store.UpdateMetric(id, m, s.tz)\n\tif accepted { s.evaluate(updated) }')
    return s
edit('internal/central/server.go',server)

def forum(s):
    # Offline is a health state, not authorization to erase an identity.
    s=once(s,'\t// Stale-node deletion is owned exclusively by staleLoop. Forum sync only\n\t// reconciles Telegram topics with the current store to avoid duplicate GC.', '\t// Reconcile saved identities with their single managed topic.')
    s=once(s,'type forumNodeState struct {','type forumNodeState struct {\n TopicPending bool `json:"topic_pending,omitempty"`\n TopicChecked time.Time `json:"topic_checked,omitempty"`')
    s=once(s,'\tst := loadForumState()','\tst,err := readForumState(forumStatePath)\n\tif err!=nil { log.Printf("forum initialization: %v",err);return }')
    s=once(s,'type forumState struct {','type forumState struct {\n OverviewTopicPending bool `json:"overview_topic_pending,omitempty"`\n OverviewTopicChecked time.Time `json:"overview_topic_checked,omitempty"`')
    start=s.index('\tif st.OverviewTopicID == 0 {')
    end=s.index('\toverview := ',start)
    s=s[:start]+'''
 topic:=telegram.LiveTopic{ID:st.OverviewTopicID,Name:st.OverviewTopicName,Pending:st.OverviewTopicPending,CheckedAt:st.OverviewTopicChecked}
 if err:=s.bot.EnsureLiveTopic(s.cfg.TelegramGroupID,"00 · 💠 Overview",&topic,func() error {
  if st.OverviewTopicID!=topic.ID { st.OverviewMessageID=0;st.OverviewLastText="";st.OverviewSendPending=false }
  st.OverviewTopicID,st.OverviewTopicName,st.OverviewTopicPending,st.OverviewTopicChecked=topic.ID,topic.Name,topic.Pending,topic.CheckedAt
  return saveForumState(*st)
 });err!=nil { return fmt.Errorf("ensure overview topic: %w",err) }
''' + s[end:]
    start=s.index('\t\tif fs.TopicID == 0 {')
    end=s.index('\t\ttext := ',start)
    s=s[:start]+'''
  topic:=telegram.LiveTopic{ID:fs.TopicID,Name:fs.TopicName,Pending:fs.TopicPending,CheckedAt:fs.TopicChecked}
  if err:=s.bot.EnsureLiveTopic(s.cfg.TelegramGroupID,name,&topic,func() error {
   if fs.TopicID!=topic.ID { fs.MessageID=0;fs.LastText="";fs.SendPending=false }
   fs.TopicID,fs.TopicName,fs.TopicPending,fs.TopicChecked=topic.ID,topic.Name,topic.Pending,topic.CheckedAt
   st.Nodes[n.ID]=fs
   return saveForumState(*st)
  });err!=nil { log.Printf("ensure node topic %s: %v",n.Name,err);continue }
''' + s[end:]
    s=once(s,'if err != nil { log.Printf("overview live card: %v", err) }','''if err != nil {
 log.Printf("overview live card: %v",err)
 if telegram.IsTopicMissing(err) { st.OverviewTopicID=0;st.OverviewMessageID=0;st.OverviewLastText="";st.OverviewSendPending=false;st.OverviewTopicPending=false;if e:=saveForumState(*st);e!=nil{return e} }
 }''')
    s=once(s,'if err != nil { log.Printf("node live card %s: %v", n.Name, err) }','''if err != nil {
 log.Printf("node live card %s: %v",n.Name,err)
 if telegram.IsTopicMissing(err) { fs.TopicID=0;fs.MessageID=0;fs.LastText="";fs.SendPending=false;fs.TopicPending=false;st.Nodes[n.ID]=fs;if e:=saveForumState(*st);e!=nil{return e} }
 }''')
    s=once(s,'if err := s.bot.DeleteForumTopic(s.cfg.TelegramGroupID, fs.TopicID); err != nil {','if err := s.bot.DeleteForumTopic(s.cfg.TelegramGroupID, fs.TopicID); err != nil && !telegram.IsTopicMissing(err) {')
    return s
edit('internal/central/forum.go',forum)
Path('internal/central/stale.go').write_text('''package central

// A long outage must not erase credentials, history or the existing topic.
// Explicit administrative merges are the only automatic topic cleanup input.
func (s *Server) staleLoop() { <-s.stop }
''',encoding='utf-8')
