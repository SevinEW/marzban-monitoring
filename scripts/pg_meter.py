"""Independent, non-resetting Xray inbound accounting; no packet capture.

Run in the existing PasarGuard Python environment. Only aggregate counters and
bounded interval history are persisted; panel credentials never leave memory.
"""
import asyncio
import json
import os
from pathlib import Path
import sqlite3
import signal
import sys
import time

ROOT = Path('/var/lib/pasarguard/marzwatch-meter')
OUTPUT = Path('/var/lib/pasarguard/marzwatch-traffic.json')
INTERVAL = 10
WINDOW = 86400


class Ledger:
    def __init__(self, path):
        self.db = sqlite3.connect(path)
        self.db.execute('PRAGMA journal_mode=WAL')
        self.db.execute('PRAGMA synchronous=FULL')
        self.db.execute('PRAGMA cache_size=-2048')
        self.db.executescript('''
        CREATE TABLE IF NOT EXISTS nodes(id INTEGER PRIMARY KEY, first REAL, last REAL, counters TEXT, gap REAL DEFAULT 0);
        CREATE TABLE IF NOT EXISTS intervals(node INTEGER, start REAL, end REAL, rx INTEGER, tx INTEGER);
        CREATE INDEX IF NOT EXISTS interval_end ON intervals(end);
        CREATE INDEX IF NOT EXISTS interval_node_end ON intervals(node,end);
        ''')
        if 'uptime' not in {r[1] for r in self.db.execute('PRAGMA table_info(nodes)')}:
            self.db.execute('ALTER TABLE nodes ADD COLUMN uptime REAL')

    def sample(self, node, stamp, counters, uptime=None):
        old = self.db.execute('SELECT first,last,counters,gap,uptime FROM nodes WHERE id=?',(node,)).fetchone()
        if old is None:
            self.db.execute('INSERT INTO nodes VALUES(?,?,?,?,?,?)',(node,stamp,stamp,json.dumps(counters),stamp,uptime))
            return
        first,last,raw,gap,previous_uptime = old
        if stamp <= last:
            return
        prev = json.loads(raw)
        rx = tx = 0
        # Backend uptime is Xray uptime, not host uptime. Detect a restarted
        # core even when its new counters already exceed the previous values.
        reset = uptime is not None and previous_uptime is not None and (
            uptime < previous_uptime or abs((stamp-last)-(uptime-previous_uptime)) > 5)
        if reset:
            gap = stamp
        # Disappearing/new counters and resets are explicitly incomplete, not
        # synthetic zero usage or the lifetime total of a newly discovered tag.
        if prev.keys() != counters.keys():
            gap = stamp
        for key,value in counters.items():
            if reset or key not in prev:
                continue
            delta = value-prev[key]
            if delta < 0:
                gap = stamp
                continue
            if key.endswith('|downlink'): rx += delta
            elif key.endswith('|uplink'): tx += delta
        if stamp-last > INTERVAL*3: gap = stamp
        self.db.execute('INSERT INTO intervals VALUES(?,?,?,?,?)',(node,last,stamp,rx,tx))
        self.db.execute('UPDATE nodes SET last=?,counters=?,gap=?,uptime=? WHERE id=?',(stamp,json.dumps(counters),gap,uptime,node))

    def totals(self, now):
        cutoff = now-WINDOW
        totals = {n:[rx,tx] for n,rx,tx in self.db.execute(
            'SELECT node,SUM(rx),SUM(tx) FROM intervals WHERE start>=? AND end<=? GROUP BY node',(cutoff,now))}
        # At most one crossing interval per node; prorate only the cutoff edge.
        for node,start,end,rx,tx in self.db.execute(
            'SELECT node,start,end,rx,tx FROM intervals WHERE start<? AND end>? AND end<=?',(cutoff,cutoff,now)):
            share = max(0,min(1,(end-cutoff)/(end-start)))
            row = totals.setdefault(node,[0,0]);row[0]+=int(rx*share);row[1]+=int(tx*share)
        out={}
        for node,first,last,gap in self.db.execute('SELECT id,first,last,gap FROM nodes'):
            recent=self.db.execute('SELECT start,end,rx,tx FROM intervals WHERE node=? ORDER BY end DESC LIMIT 1',(node,)).fetchone()
            rxps=txps=0
            if recent and now-last<=35:
                start,end,rx,tx=recent
                rxps,txps=rx/(end-start),tx/(end-start)
            rx,tx=totals.get(node,(0,0))
            out[str(node)]={'rx':rx,'tx':tx,'rx_bps':rxps,'tx_bps':txps,'last':last,
                            'complete':first<=cutoff and gap<=cutoff,'since':max(first,gap),
                            'as_of':now}
        return out

    def commit(self, now):
        self.db.execute('DELETE FROM intervals WHERE end<?',(now-WINDOW-3600,))
        self.db.execute('DELETE FROM nodes WHERE last<?',(now-WINDOW-3600,))
        self.db.commit()


def publish(data):
    tmp=OUTPUT.with_suffix('.tmp')
    with open(tmp,'w') as f:
        json.dump(data,f,separators=(',',':'));f.flush();os.fsync(f.fileno())
    os.chmod(tmp,0o644)
    os.replace(tmp,OUTPUT)


def snapshot(ledger, metadata, now):
    # Removing/disabling a node must not erase bytes it used inside the window.
    values=ledger.totals(now)
    for node in values:
        values[node].update(metadata.get(node,{}))
        values[node]['active']=node in metadata
    for node,meta in metadata.items():
        if node not in values:
            values[node]=dict(meta,rx=0,tx=0,rx_bps=0,tx_bps=0,last=0,complete=False,active=True)
    return {'schema':1,'source':'xray-inbound-no-reset','window_seconds':WINDOW,'generated':now,'nodes':values}


async def run():
    import fcntl
    import asyncpg
    from types import SimpleNamespace
    from PasarGuardNodeBridge import create_node,NodeType
    from PasarGuardNodeBridge.common.service_pb2 import StatType
    ROOT.mkdir(mode=0o700,parents=True,exist_ok=True)
    os.umask(0o077)
    lock = open(ROOT/'meter.lock','a')
    fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
    (ROOT/'meter.pid').write_text(str(os.getpid()))
    os.nice(10)
    ledger=Ledger(ROOT/'traffic.sqlite')
    # Importing app.db initializes the entire panel application. Use one
    # bounded SQL connection and fetch only the small node/core inventory.
    url=os.environ['SQLALCHEMY_DATABASE_URL']
    if not url.startswith(('postgresql://','postgresql+asyncpg://')):
        raise RuntimeError('This adapter requires a PostgreSQL PasarGuard deployment')
    url=url.replace('postgresql+asyncpg://','postgresql://',1)
    clients={}; metadata={}; retry={}; refresh=0; sem=asyncio.Semaphore(3)
    async def close_client(client):
        # Never call stop(): it can stop the remote proxy core.
        await client.disconnect()
        channel=getattr(client,'channel',None)
        if channel is not None:await channel.close()
        http=getattr(client,'_json_client',None)
        if http is not None:await http.close()
    while True:
        started=time.monotonic();now=time.time()
        if now>=refresh:
            try:
                async with asyncio.timeout(8):
                    db=await asyncpg.connect(url,timeout=4,command_timeout=4,statement_cache_size=0)
                    try:
                        nodes=await db.fetch('''SELECT n.id,n.name,n.address,n.port,n.api_port,
                        n.server_ca,n.api_key,n.connection_type,n.proxy_url,c.config
                        FROM nodes n JOIN core_configs c ON c.id=n.core_config_id
                        WHERE c.type='xray' AND n.status<>'disabled' ''')
                    finally:
                        await db.close()
                    fresh={};meta={}
                    for row in nodes:
                        n=SimpleNamespace(**row)
                        config=json.loads(n.config) if isinstance(n.config,str) else n.config
                        key=(n.address,n.port,n.api_port,n.server_ca,n.api_key,n.connection_type,n.proxy_url)
                        prior=clients.get(n.id)
                        if prior and prior[0]==key: client=prior[1]
                        else:
                            client=create_node(connection=NodeType.grpc if n.connection_type=='grpc' else NodeType.rest,
                                address=n.address,port=n.port,api_port=n.api_port,server_ca=n.server_ca,api_key=n.api_key,
                                name='traffic-meter',default_timeout=4,internal_timeout=4,proxy=n.proxy_url)
                        tags={i['tag'] for i in config.get('inbounds',[]) if 'tag' in i}
                        fresh[n.id]=(key,client,tags)
                        meta[str(n.id)]={'name':n.name,'address':n.address}
                    retired=[entry[1] for ident,entry in clients.items()
                             if ident not in fresh or fresh[ident][1] is not entry[1]]
                    clients,metadata=fresh,meta
                    retry={n:r for n,r in retry.items() if n in clients}
                    for client in retired:
                        try:await asyncio.wait_for(close_client(client),2)
                        except Exception:pass
                refresh=now+60
            except Exception as e:
                print('meter inventory unavailable: '+type(e).__name__,flush=True)
                refresh=now+30
        async def collect(node,entry):
            failures,next_try=retry.get(node,(0,0))
            if time.time()<next_try:return
            async with sem:
                try:
                    response=await asyncio.wait_for(entry[1].get_stats(stat_type=StatType.Inbounds,reset=False,timeout=4),5)
                    backend=await asyncio.wait_for(entry[1].get_backend_stats(timeout=4),5)
                    counters={}
                    for stat in response.stats:
                        if stat.name in entry[2] and stat.type in ('uplink','downlink'):
                            value=int(stat.value)
                            if value<0:raise ValueError('negative counter')
                            key=stat.name+'|'+stat.type
                            if key in counters:raise ValueError('duplicate counter')
                            counters[key]=value
                    if not counters:raise ValueError('inbound counters unavailable')
                    ledger.sample(node,time.time(),counters,backend.uptime);retry[node]=(0,0)
                except Exception as e:
                    failures+=1;retry[node]=(failures,time.time()+min(60,INTERVAL*2**min(failures-1,3)))
                    if failures==1:print('meter node '+str(node)+' unavailable: '+type(e).__name__,flush=True)
        await asyncio.gather(*(collect(n,e) for n,e in clients.items()))
        now=time.time();ledger.commit(now)
        publish(snapshot(ledger,metadata,now))
        await asyncio.sleep(max(1,INTERVAL-(time.monotonic()-started)))


def stop():
    try:
        pid=int((ROOT/'meter.pid').read_text())
        args=Path('/proc',str(pid),'cmdline').read_bytes().split(b'\0')
        if os.fsencode(str(Path(__file__).resolve())) not in args:
            raise RuntimeError('meter PID no longer belongs to meter')
        os.kill(pid,signal.SIGTERM)
    except (FileNotFoundError,ProcessLookupError):
        pass


if __name__=='__main__':
    if sys.argv[1:]==['--stop']:stop()
    elif sys.argv[1:]:raise SystemExit('usage: pg_meter.py [--stop]')
    else:asyncio.run(run())
