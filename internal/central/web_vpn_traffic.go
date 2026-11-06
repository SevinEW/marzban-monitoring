package central

import (
 "encoding/json"
 "io"
 "math"
 "os"
 "time"
)

func readTrafficBindings(configured map[string]string) map[string]string {
 bindings:=map[string]string{}
 // Central's explicitly configured identity is separate from auto enrollment.
 if id:=configured["central"];id!="" {bindings["central"]=id}
 f,err:=os.Open("/etc/marzwatch/presence-auto-scope.json");if err!=nil{return bindings};defer f.Close()
 b,err:=io.ReadAll(io.LimitReader(f,1048577));if err!=nil||len(b)>1048576{return bindings}
 var scope struct{Nodes map[string]string `json:"traffic_nodes"`}
 if json.Unmarshal(b,&scope)!=nil{return bindings}
 for mid,pid:=range scope.Nodes {if mid!="central"&&pid!=""{bindings[mid]=pid}}
 return bindings
}

func applyVPNVolume(w *webNode,id string,v pgSnapshot,valid bool,now time.Time) {
 // Never fall back to NIC bytes or a guessed half when a VPN source is absent.
 w.Traffic,w.TrafficRX,w.TrafficTX=0,0,0;w.Complete=false;w.TrafficAvailable=false
 w.TrafficSource="vpn";w.TrafficID=id;w.TrafficStatus="unavailable"
 if id=="" {w.TrafficStatus="unmapped";if w.Presence.Status=="not_applicable"{w.TrafficStatus="not_applicable"};return}
 u,exists:=v.Nodes[id];t:=float64(now.UnixNano())/1e9
 if !valid||!exists||u.Last<=0||u.Last>t+5||u.RX>math.MaxUint64-u.TX||u.Since<=0||u.Since>t+5{return}
 w.TrafficRX=float64(u.RX);w.TrafficTX=float64(u.TX);w.Traffic=w.TrafficRX+w.TrafficTX
 w.TrafficAvailable=true;w.TrafficSince=u.Since
 w.Complete=u.Complete&&t-u.Last<=35&&u.Since<=t-86400
 switch {case t-u.Last>35:w.TrafficStatus="stale";case w.Complete:w.TrafficStatus="complete";case u.Reason=="warming_up"||u.Reason=="":w.TrafficStatus="warming_up";default:w.TrafficStatus="interrupted"}
}
