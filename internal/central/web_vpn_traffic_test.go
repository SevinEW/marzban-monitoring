package central

import (
 "testing"
 "time"
 "github.com/SevinEW/marzban-monitoring/internal/presence"
)

func TestWebVPNVolumeNeverImportsHostTraffic(t *testing.T) {
 now:=time.Unix(200000,0)
 v:=pgSnapshot{Nodes:map[string]pgUsage{"1":{RX:120,TX:30,Last:200000,Since:100000,Complete:true}}}
 for _,tc:=range []struct{name,id string;valid bool;status string;available bool}{
  {"valid","1",true,"complete",true},{"stale handoff","1",false,"unavailable",false},{"no binding","",true,"unmapped",false},{"missing counters","2",true,"unavailable",false},
 } {t.Run(tc.name,func(t *testing.T){
  w:=webNode{Traffic:999999,TrafficRX:888888,TrafficTX:111111,Complete:true,TrafficAvailable:true,Presence:presence.View{Status:"ok"}}
  applyVPNVolume(&w,tc.id,v,tc.valid,now)
  if w.TrafficStatus!=tc.status||w.TrafficAvailable!=tc.available {t.Fatal(w)}
  if tc.available {if w.Traffic!=150||w.TrafficRX!=120||w.TrafficTX!=30||!w.Complete {t.Fatal(w)}} else if w.Traffic!=0||w.Complete{t.Fatal("host bytes leaked",w)}
 })}
 w:=webNode{Presence:presence.View{Status:"not_applicable"}};applyVPNVolume(&w,"",v,true,now);if w.TrafficStatus!="not_applicable"{t.Fatal(w)}
 u:=v.Nodes["1"];u.Last=199900;v.Nodes["1"]=u;applyVPNVolume(&w,"1",v,true,now)
 if w.Complete||!w.TrafficAvailable||w.TrafficStatus!="stale"||w.Traffic!=150{t.Fatal("offline retained volume lost",w)}
 u.Last=200000;u.Since=199000;u.Complete=false;u.Reason="warming_up";v.Nodes["1"]=u;applyVPNVolume(&w,"1",v,true,now)
 if w.Complete||w.TrafficStatus!="warming_up"||w.TrafficSince!=199000{t.Fatal("partial history hidden",w)}
}
