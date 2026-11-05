package central

import (
 "encoding/json"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
 "time"
 "github.com/SevinEW/marzban-monitoring/internal/config"
 "github.com/SevinEW/marzban-monitoring/internal/model"
 "github.com/SevinEW/marzban-monitoring/internal/storage"
)
func TestWebSnapshotExcludesSecretsAndMarksStale(t *testing.T){
 st,err:=storage.Open(filepath.Join(t.TempDir(),"state.json"));if err!=nil{t.Fatal(err)}
 now:=time.Now();st.UpsertNode(model.Node{ID:"node",Name:"test",Secret:"NEVER-EXPOSE-SECRET",ServerKey:"PRIVATE-MACHINE-KEY",LastSeen:now.Add(-11*time.Second),Latest:model.Metric{RXBps:2e9,NetworkSource:model.NetworkSource,NetworkReady:true}})
 s:=&Server{store:st};f:=s.webSnapshot(now);b,_:=json.Marshal(f)
 if strings.Contains(string(b),"SECRET")||strings.Contains(string(b),"MACHINE")||strings.Contains(string(b),"secret"){t.Fatal("private identity leaked")}
 if len(f.Nodes)!=1||f.Nodes[0].Status!="stale"||f.Nodes[0].RX!=2e9{t.Fatal("freshness or rate units incorrect")}
 f=s.webSnapshot(now.Add(3*time.Minute));if f.Nodes[0].Status!="offline"||f.Nodes[0].RX!=0{t.Fatal("offline rate shown as live")}
}
func TestWebSnapshotExposesStoredVolumeDirections(t *testing.T) {
	st, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil { t.Fatal(err) }
	now := time.Unix(100000, 0)
	st.UpsertNode(model.Node{ID:"volume", LastSeen:now, Latest:model.Metric{RXBps:999999, TXBps:888888}})
	st.UpsertNode(model.Node{ID:"missing", LastSeen:now})
	h := &hostTraffic{data:hostDisk{Version:2, Nodes:map[string]*hostNode{
		"volume": {First:0, Last:100000, Buckets:[]hostBucket{{Start:99990, End:100000, RX:120, TX:0}}},
	}}}
	s := &Server{store:st, host:h}
	f := s.webSnapshot(now)
	for _, n := range f.Nodes {
		if n.ID == "missing" { if n.TrafficAvailable || n.Complete { t.Fatal("missing volume exposed as measured zero") }; continue }
		if !n.TrafficAvailable || !n.Complete || n.TrafficRX != 120 || n.TrafficTX != 0 || n.Traffic != 120 { t.Fatalf("volume confused with rates: %+v", n) }
		b, err := json.Marshal(n); if err != nil { t.Fatal(err) }
		if !strings.Contains(string(b), `"traffic_rx":120`) || !strings.Contains(string(b), `"traffic_tx":0`) { t.Fatal("zero direction omitted from wire format") }
	}
}

func TestWebHistoryBoundedAndNotifies(t *testing.T){
 h:=&webHub{};h.publish(webFrame{Time:1});changed:=h.changed
 for i:=2;i<=500;i++{h.publish(webFrame{Time:int64(i)})}
 select{case <-changed:default:t.Fatal("viewer not notified")}
 if len(h.history)!=360{t.Fatal("unbounded history")}
 var first webFrame;if err:=json.Unmarshal(h.history[0],&first);err!=nil||first.Time!=141{t.Fatal("old history retained")}
}
func TestWebRejectsPublicBindAndExcessViewers(t *testing.T){
 s:=&Server{cfg:config.Config{WebListen:"0.0.0.0:9088"}};if s.startWebPanel()==nil{t.Fatal("unauthenticated public bind allowed")}
 h:=&webHub{clients:make(chan struct{},1)};h.clients<-struct{}{}
 w:=httptest.NewRecorder();h.stream(w,httptest.NewRequest("GET","/api/stream",nil));if w.Code!=503{t.Fatal("viewer limit ignored")}
}
