"""Restore operator-requested one-hour expiry and refresh changing public IPs."""
from pathlib import Path

def once(s,old,new):
 if s.count(old)!=1:raise SystemExit(f'Expiry/IP anchor missing: {old[:100]!r}')
 return s.replace(old,new,1)

def edit(path,fn):
 p=Path(path);p.write_text(fn(p.read_text(encoding='utf-8')),encoding='utf-8')

edit('internal/model/model.go',lambda s:once(s,'type Node struct {','type Node struct {\n LastContact time.Time `json:"last_contact,omitempty"`'))

def identity(s):
 s=once(s,'\tfor from, to := range next {','''
 // Config can still contain an explicit merge whose entire group has expired.
 // Do not resurrect it or fail startup after the database transaction removed it.
 for from,to:=range next {if s.nodes[from]==nil && s.nodes[to]==nil {delete(next,from)}}
 for from, to := range next {''')
 return s
edit('internal/storage/identity.go',identity)

def server(s):
 s=once(s,'\tgo s.supervise("location-refresh", s.locationLoop)','\tgo s.supervise("local-ip-refresh", s.localIPLoop)\n\tgo s.supervise("location-refresh", s.locationLoop)')
 s=once(s,'Registered: time.Now(), LastSeen: time.Now(),','Registered: time.Now(), LastSeen: time.Now(), LastContact:time.Now(),')
 s=once(s,'\tif err:=s.store.BindServerKey(id,m.ServerKey);err!=nil', '\tif !s.store.RecordContact(id,time.Now()) {http.Error(w,"unknown node",401);return}\n\tif err:=s.store.BindServerKey(id,m.ServerKey);err!=nil')
 s=once(s,'\ts.store.SetPublicIP(id, geo.PublicIP(m.PublicIP))','\ts.observePublicIP(id,m.PublicIP)')
 # Existing authenticated receipt is enough to distinguish clock correction
 # from an offline node; bad signatures never reach RecordContact.
 return s
edit('internal/central/server.go',server)
edit('internal/agent/agent.go',lambda s:once(s,'\tgo func() { a.publicIP.Store(geo.Detect().PublicIP) }()', '\tgo a.publicIPLoop()'))

def geo(s):
 s=once(s,'func Detect() model.Location {','func Detect() model.Location { return Lookup(DetectIP()) }\n\n// Address discovery must not wait for country databases on each refresh.\nfunc DetectIP() string {')
 start=s.index('func DetectIP() string {');end=s.index('\nfunc Lookup(',start)
 block=s[start:end].replace('return Lookup(ip)','return ip').replace('return model.Location{}','return ""')
 return s[:start]+block+s[end:]
edit('internal/geo/resolver.go',geo)

def forum(s):
 s=once(s,'\tfor _, n := range nodes {\n\t\tseen[n.ID] = true','\tfor _, n := range nodes {\n\t\tif _,exists:=s.store.GetNode(n.ID);!exists {continue}\n\t\tseen[n.ID] = true')
 s=once(s,'\t// Clean orphan forum mappings', '\t// Nodes may expire while Telegram writes wait in the rate-limit queue.\n\tseen=map[string]bool{}\n\tfor _,n:=range s.store.Nodes() {seen[n.ID]=true}\n\t// Clean orphan forum mappings')
 return s
edit('internal/central/forum.go',forum)

Path('internal/central/stale.go').write_text('''package central

import ("log";"time")

func (s *Server) staleLoop() {
 // Allow reconnecting nodes to report after a Central restart. Subsequent
 // checks remove nodes strictly over one hour since the last valid receipt.
 t:=time.NewTicker(time.Minute);defer t.Stop()
 for {select {case <-s.stop:return;case now:=<-t.C:s.removeStaleNodes(now)}}
}

func (s *Server) removeStaleNodes(now time.Time) {
 removed,err:=s.store.Expire(now,time.Hour)
 if err!=nil {log.Printf("offline expiry persistence failed: %v",err);return}
 for _,n:=range removed {
  s.alertsMu.Lock();delete(s.alerts,n.ID);s.alertsMu.Unlock()
  log.Printf("node removed after more than one hour offline: %s",n.Name)
 }
 // Forum's existing orphan cleanup owns Telegram deletion and retries failures.
}
''',encoding='utf-8')
