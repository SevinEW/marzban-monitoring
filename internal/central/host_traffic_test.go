package central

import (
 "math"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"path/filepath"
"os"
	"strings"
	"testing"
	"time"
)

func hostTestNode(id string, stamp int64, rx uint64) model.Node {
	t := time.Unix(stamp, 0)
	return model.Node{ID: id, LastContact: t, LastSeen: t, Latest: model.Metric{NetworkSource:model.NetworkSource, NetworkBoot:"boot", NetworkReady:true, NetworkCounters:[]model.NetCounter{{Name:"eth0",Index:2,RX:rx,TX:rx/2}}, Timestamp: t, RXBytesTotal: rx, TXBytesTotal: rx / 2, UptimeSeconds: uint64(stamp), RXBps: 1000000000, TXBps: 1000000000}}
}
func TestHostBreakdownClipsBothDirectionsInOneWindow(t *testing.T) {
	p := &hostNode{First: 0, Last: 86430, Buckets: []hostBucket{
		{Start: 0, End: 60, RX: 600, TX: 120},
		{Start: 86400, End: 86460, RX: 60, TX: 300},
		{Start: 86460, End: 86520, RX: 9999, TX: 9999},
	}}
	rx, tx, complete := hostBreakdown(p, 86430)
	if rx != 330 || tx != 210 || !complete {
		t.Fatalf("wrong clipped directions: rx=%v tx=%v complete=%v", rx, tx, complete)
	}
	if total, ok := hostSum(p, 86430); total != rx+tx || ok != complete {
		t.Fatal("legacy total differs from directional total")
	}
	p.Gap = 86400
	if _, _, ok := hostBreakdown(p, 86430); ok { t.Fatal("gap hidden by directional display") }
	if rx, tx, ok := hostBreakdown(nil, 86430); rx != 0 || tx != 0 || ok { t.Fatal("missing history reported as complete") }
}

func TestHostRollingWindowAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.json")
	h, err := openHostTraffic(path)
	if err != nil {
		t.Fatal(err)
	}
	h.sample([]model.Node{hostTestNode("iran", 100000, 1000)}, time.Unix(100000, 0))
	h.sample([]model.Node{hostTestNode("iran", 100010, 2000)}, time.Unix(100010, 0))
	if got := h.value("iran", time.Unix(100010, 0)); got != bytes(1500)+" *" {
		t.Fatal(got)
	}
	if err := h.save(); err != nil {
		t.Fatal(err)
	}
	h, err = openHostTraffic(path)
	if err != nil {
		t.Fatal(err)
	}
	h.sample([]model.Node{hostTestNode("iran", 100010, 2000)}, time.Unix(100015, 0))
	if got := h.value("iran", time.Unix(100015, 0)); got != bytes(1500)+" *" {
		t.Fatal(got)
	}
	total, _ := hostSum(h.data.Nodes["iran"], 186405)
	if total != 750 {
		t.Fatal(total)
	}
	total, _ = hostSum(h.data.Nodes["iran"], 186410)
	if total != 0 {
		t.Fatal(total)
	}
}
func TestHostResetNewNodeAndBoundedBuckets(t *testing.T) {
	h, _ := openHostTraffic(filepath.Join(t.TempDir(), "host.json"))
	for i := int64(0); i <= 90000; i += 5 {
		stamp := 100000 + i
		h.sample([]model.Node{hostTestNode("one", stamp, uint64(i)*100)}, time.Unix(stamp, 0))
	}
	if len(h.data.Nodes["one"].Buckets) > 1443 {
		t.Fatal(len(h.data.Nodes["one"].Buckets))
	}
	total, complete := hostSum(h.data.Nodes["one"], 190000)
	if total != 86400*150 || !complete {
		t.Fatal(total, complete)
	}
	h.sample([]model.Node{hostTestNode("one", 190005, 10), hostTestNode("new", 190005, 999999999)}, time.Unix(190005, 0))
	if h.value("new", time.Unix(190005, 0)) != bytes(0)+" *" {
		t.Fatal("new lifetime imported")
	}
	if !strings.HasSuffix(h.value("one", time.Unix(190005, 0)), " *") {
		t.Fatal("reset not marked")
	}
}
func TestHostUsesExistingBitRatesWithoutPanelMapping(t *testing.T) {
	h, _ := openHostTraffic(filepath.Join(t.TempDir(), "host.json"))
	now := time.Now()
	n := hostTestNode("iran", now.Unix(), 10)
	h.sample([]model.Node{n}, now)
	s := &Server{host: h}
	out := s.formatHostNode(n, true, "2026-10-16")
	if strings.Contains(out, "N/A") || !strings.Contains(out, "2.00 Gbps") || !strings.Contains(out, "24H") {
		t.Fatal(out)
	}
}

func TestHostArchivesIncompatibleSeriesAndRejectsOldAgents(t *testing.T) {
 path:=filepath.Join(t.TempDir(),"host.json")
 old:=[]byte(`{"Version":1,"Nodes":{"old":{"First":1,"RX":999999999}}}`)
 if e:=os.WriteFile(path,old,0600);e!=nil{t.Fatal(e)}
 h,e:=openHostTraffic(path);if e!=nil{t.Fatal(e)}
 b,e:=os.ReadFile(path+".all-interfaces-v1.bak");if e!=nil||string(b)!=string(old){t.Fatal(e,string(b))}
 if h.data.Version!=2||len(h.data.Nodes)!=0{t.Fatal("legacy series reused")}
 n:=hostTestNode("old",100000,99999999);n.Latest.NetworkSource=""
 h.sample([]model.Node{n},time.Unix(100000,0));if len(h.data.Nodes)!=0{t.Fatal("old agent accepted")}
}
func TestHostTopologyChangesDoNotImportLifetimeOrMaskReset(t *testing.T) {
 h,_:=openHostTraffic(filepath.Join(t.TempDir(),"host.json"))
 a:=hostTestNode("n",100000,1000);h.sample([]model.Node{a},a.LastContact)
 b:=hostTestNode("n",100005,2000);b.Latest.NetworkCounters=append(b.Latest.NetworkCounters,model.NetCounter{Name:"eth1",Index:3,RX:999999999})
 h.sample([]model.Node{b},b.LastContact)
 total,complete:=hostSum(h.data.Nodes["n"],100005);if total!=0||complete{t.Fatal(total,complete)}
 c:=hostTestNode("n",100010,3000);c.Latest.NetworkCounters=append(c.Latest.NetworkCounters,model.NetCounter{Name:"eth1",Index:3,RX:10})
 h.sample([]model.Node{c},c.LastContact)
 total,_=hostSum(h.data.Nodes["n"],100010);if total!=0{t.Fatal("reset hidden",total)}
}

func TestHostCheckpointResumePreservesProvenVolume(t *testing.T) {
 for _, tc := range []struct{name string; delay int64; reset, reboot, topology, partial bool; wantGap bool}{
  {name:"checkpoint lag",delay:35},
  {name:"bounded checkpoint lag",delay:60},
  {name:"long outage",delay:61,wantGap:true},
  {name:"counter reset",delay:35,reset:true,wantGap:true},
  {name:"reboot",delay:35,reboot:true,wantGap:true},
  {name:"interface changed",delay:35,topology:true,wantGap:true},
  {name:"existing gap retained",delay:35,partial:true},
 } { t.Run(tc.name,func(t *testing.T){
  path:=filepath.Join(t.TempDir(),"host.json");h,err:=openHostTraffic(path);if err!=nil{t.Fatal(err)}
  n:=hostTestNode("n",100000,1000);h.sample([]model.Node{n},n.LastContact)
  h.data.Nodes["n"].First=0;h.data.Nodes["n"].Gap=0
  if tc.partial {h.data.Nodes["n"].Gap=99990}
  priorGap:=h.data.Nodes["n"].Gap
  if err=h.save();err!=nil{t.Fatal(err)}
  h,err=openHostTraffic(path);if err!=nil{t.Fatal(err)}
  // A cached duplicate frame must not consume the one-time resume allowance.
  h.sample([]model.Node{n},time.Unix(100001,0))
  next:=hostTestNode("n",100000+tc.delay,3000)
  if tc.reset {next.Latest.NetworkCounters[0].RX=10}
  if tc.reboot {next.Latest.NetworkBoot="new-boot"}
  if tc.topology {next.Latest.NetworkCounters[0].Index++}
  h.sample([]model.Node{next},next.LastContact)
  p:=h.data.Nodes["n"];expectedGap:=priorGap;if tc.wantGap{expectedGap=float64(100000+tc.delay)}
  if p.Gap!=expectedGap {t.Fatalf("gap=%v want=%v",p.Gap,expectedGap)}
  rx,tx,complete:=hostBreakdown(p,float64(100000+tc.delay))
  if complete!=( !tc.wantGap && !tc.partial ){t.Fatalf("complete=%v",complete)}
  if tc.reset||tc.reboot||tc.topology {if rx!=0||tx!=0{t.Fatal("invalid baseline imported",rx,tx)}} else if math.Abs(rx-2000)>1e-6||math.Abs(tx-1000)>1e-6 {t.Fatal("checkpoint delta lost or duplicated",rx,tx)}
  if h.resume["n"] {t.Fatal("resume allowance was not consumed")}
  later:=hostTestNode("n",100000+tc.delay+35,4000)
  h.sample([]model.Node{later},later.LastContact)
  if p.Gap!=float64(later.LastContact.Unix()) {t.Fatal("ordinary sampling gap hidden by restart grace")}
 }) }
}
