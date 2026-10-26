const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const app=fs.readFileSync('internal/central/web/app.js','utf8').split('// DOM application.')[0];
const cx=vm.createContext({});vm.runInContext(app+';globalThis.math={aggregate,segments,rate,bytes,niceMax,rateAxis,compareNodes};',cx);const m=cx.math;
const fresh=(v={})=>({status:'online',cpu:50,cores:4,rx:2e9,tx:1e9,ram:2,ram_total:8,disk:10,disk_total:100,...v});
const result=m.aggregate([fresh(),fresh({cpu:100,cores:1,rx:1e9,tx:0}),fresh({status:'stale',rx:9e9}),fresh({status:'offline',rx:99e9})]);
assert.equal(result.a,3e9);assert.equal(result.c,4e9);assert.equal(result.cpu,60);assert.equal(result.count,2);assert.equal(result.total,4);assert.equal(result.ram,25);
assert.equal(m.aggregate([fresh({status:'offline'})]).a,null);assert.equal(m.aggregate([fresh({cores:0})]).cpu,null);
assert.equal(m.rate(2e9).join(' '),'2 Gbps');assert.equal(m.bytes(1024**3).join(' '),'1 GiB');
const parts=m.segments([{t:0,a:2},{t:1000,a:3},{t:2000,a:null},{t:3000,a:5},{t:9000,a:6}],'a');assert.equal(parts.length,3);assert.equal(parts[0].length,2);assert.equal(parts[1][0].a,5);
assert.equal(m.rateAxis(2e9).divisor,1e9);assert.equal(m.rateAxis(2e9).unit,'Gbps');assert(m.niceMax(1.81e9)>=1.81e9);
console.log('PASS: bit-rate units, weighted CPU, capacity ratios, stale exclusion, no-data semantics, gap segmentation, binary volume units, fixed axis units');
// Repeated identical SVG markup must not be reassigned after DOM serialization.
const source=fs.readFileSync('internal/central/web/app.js','utf8');
const cache=source.slice(source.indexOf('const renderedHTML='),source.indexOf('function compact('));
vm.runInContext(cache+';globalThis.writeHTML=writeHTML;',cx);
let assignments=0,serialized='';const element={get innerHTML(){return serialized},set innerHTML(v){assignments++;serialized=v.replace('/>','></use>')}};
cx.writeHTML(element,'<use href="/flags.svg#DE"/>');cx.writeHTML(element,'<use href="/flags.svg#DE"/>');assert.equal(assignments,1);
cx.writeHTML(element,'<use href="/flags.svg#FI"/>');assert.equal(assignments,2);console.log('PASS: identical SVG survives repeated live frames; changed country updates');

const nodes=[
{id:'a',name:'Node2',code:'DE',status:'online',traffic_available:true,traffic:200,rx:20,tx:30},
{id:'b',name:'Node1',code:'NL',status:'online',traffic_available:true,traffic:500,rx:10,tx:10},
{id:'c',name:'Node3',code:'TR',status:'stale',traffic_available:false,traffic:9999,rx:9999,tx:9999}
];
assert.equal([...nodes].sort((a,b)=>m.compareNodes(a,b)).map(n=>n.id).join(','),'b,a,c');
assert.equal([...nodes].sort((a,b)=>m.compareNodes(a,b,'rate')).map(n=>n.id).join(','),'a,b,c');
assert.equal([...nodes].sort((a,b)=>m.compareNodes(a,b,'country')).map(n=>n.id).join(','),'a,b,c');
assert(m.compareNodes({...nodes[0],name:'Node2'},{...nodes[0],name:'Node10'})<0);
assert.equal(m.compareNodes(nodes[0],nodes[0]),0);
console.log('PASS: volume default descending, RX+TX rate order, unavailable data last, deterministic ties');

vm.runInContext('globalThis.presence={presenceValue,presenceDescription};',cx);
for(const status of ['stale','partial','unavailable','source_changed'])assert.equal(cx.presence.presenceValue({users:999,status}).users,'—');
assert.equal(cx.presence.presenceValue({users:0,status:'ok'}).users,'0');
assert.equal(cx.presence.presenceValue({users:-1,status:'ok'}).users,'—');
assert.equal(cx.presence.presenceValue({users:Infinity,status:'ok'}).users,'—');
console.log('PASS: presence zero, invalid data, incomplete coverage and stale status');

// Incomplete fleet coverage must show the observed count without posing as a total.
const view=cx.presence.presenceValue,description=cx.presence.presenceDescription;
const partial={status:'partial',users:null,observed_users:0,ready:1,expected:17};
assert.equal(view(partial,true).users,'0');assert.equal(view(partial,true).status,'partial');
assert.equal(view(partial,true).coverage,'1 از 17 سرور');
assert(description(partial,true).includes('این عدد مجموع کل شبکه نیست'));
assert.equal(view({...partial,users:999,observed_users:4},true).users,'4');
assert.equal(view({...partial,ready:0},true).users,'—');
assert.equal(view({...partial,ready:18},true).users,'—');
assert.equal(view({...partial,expected:0},true).users,'—');
for(const bad of [null,-1,NaN,Infinity,1.5,'3',Number.MAX_SAFE_INTEGER+1]){
 assert.equal(view({...partial,observed_users:bad},true).users,'—');
}
assert.equal(view({...partial,status:'stale',observed_users:9000},true).users,'—');
assert.equal(view({status:'ok',users:0,ready:17,expected:17},true).users,'0');
assert.equal(view({status:'ok',users:123,ready:1,expected:17},true).users,'—');
assert.equal(view(partial,false).users,'—');
// Exercise the real renderer through reconnect/coverage changes and repeated frames.
const renderer=source.slice(source.indexOf('function renderPresence('),source.indexOf('function render(){'));
vm.runInContext(renderer+';globalThis.renderPresence=renderPresence;',cx);
let writes=0;const field=()=>({value:'',get textContent(){return this.value},set textContent(v){writes++;this.value=v}});
const count=field(),coverage=field(),attrs={};
const badge={dataset:{},title:'',querySelector:s=>s==='.presence-count'?count:coverage,getAttribute:k=>attrs[k],setAttribute:(k,v)=>attrs[k]=v};
cx.renderPresence(badge,partial,true);assert.equal(count.textContent,'0');assert.equal(coverage.textContent,'1 از 17 سرور');
assert.equal(badge.dataset.presence,'partial');const once=writes;
cx.renderPresence(badge,partial,true);assert.equal(writes,once,'identical frame rewrites count');
cx.renderPresence(badge,{status:'stale'},true);assert.equal(count.textContent,'—');
assert(!coverage.textContent.includes('17'),'stale count retains old coverage');
cx.renderPresence(badge,{status:'ok',users:9,ready:17,expected:17},true);assert.equal(count.textContent,'9');
assert.equal(badge.dataset.presence,'ok');assert(!badge.title.includes('مجموع کل شبکه نیست'));
console.log('PASS: partial counts, explicit coverage, true zero, invalid inputs, disconnect/reconnect and stable badge updates');
