#!/usr/bin/env python3
"""Opt-in declarative enrollment through the authenticated monitoring link.

No SSH to agents, remote commands or user creation. Optional Central automation
prepares a private core and may restart a newly matched node once when approved.
Central periodically refreshes minimal plans with a bounded read-only snapshot.
Agents execute release-embedded verification and publish only proven bindings.
"""
import hashlib
import hmac
import http.client
import ipaddress
import json
import os
from pathlib import Path
import re
import ssl
import time
from urllib.parse import urlsplit
import recover_pasarguard_presence as r
from presence_source import minimal_source

CONFIG = Path('/etc/marzwatch/config.json')
IDENTITY = Path('/var/lib/marzwatch/identity.json')
FLEET = Path('/etc/marzwatch/presence-fleet-config.json')
PLANS = Path('/etc/marzwatch/presence-fleet-plans.json')
LOCAL = Path('/var/lib/marzwatch-updater/presence-enrollment.json')
PROFILE = Path('/etc/marzwatch/presence-recovery.json')


def root_json(path):
    st = path.stat()
    r.require(st.st_uid == 0 and st.st_mode & 0o022 == 0, 'unsafe_configuration')
    return json.loads(path.read_text())


def peer_host(cfg):
    u = urlsplit(cfg['central_url'])
    r.require(u.scheme == 'https' and u.path in ['', '/'] and not u.query and not u.fragment
              and u.username is None and u.password is None, 'invalid_central_url')
    return str(ipaddress.IPv4Address(u.hostname)), u.port or 443


def exchange(cfg, ident, report):
    host, port = peer_host(cfg)
    pin = cfg.get('cert_fingerprint', '').lower()
    r.require(re.fullmatch('[a-f0-9]{64}', pin), 'invalid_tls_pin')
    body = json.dumps(report).encode()
    r.require(len(body) <= 16384, 'report_too_large')
    stamp = str(int(time.time()))
    sig = hmac.new(ident['node_secret'].encode(), b'presence-enroll-v1\n'+stamp.encode()+b'\n'+body, hashlib.sha256).hexdigest()
    # The installation already pins Central's self-signed certificate. Verify
    # that exact pin BEFORE sending any identity, signature or request body.
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    conn = http.client.HTTPSConnection(host, port, timeout=8, context=context)
    try:
        conn.connect()
        actual = hashlib.sha256(conn.sock.getpeercert(binary_form=True)).hexdigest()
        r.require(hmac.compare_digest(pin, actual), 'central_certificate_mismatch')
        conn.request('POST', '/api/v1/presence-enroll', body=body, headers={
            'Content-Type': 'application/json', 'X-MW-Node': ident['node_id'],
            'X-MW-Time': stamp, 'X-MW-Signature': sig})
        response = conn.getresponse()
        data = response.read(65537)
        r.require(len(data) <= 65536, 'plan_too_large')
        if response.status in [204, 403]:
            return None
        r.require(response.status == 200, 'central_unavailable')
        return json.loads(data)
    finally:
        conn.close()


def local_addresses():
    rows = json.loads(r.run(['ip', '-j', '-4', 'addr', 'show']))
    return sorted({str(ipaddress.IPv4Address(a['local'])) for row in rows
                   for a in row.get('addr_info', []) if a.get('scope') == 'global'})[:32]


def inventory():
    # Inspect metadata only. Do not execute an unapproved core during discovery.
    ids = r.run(['docker', 'ps', '-q']).split()
    r.require(len(ids) <= 40, 'too_many_containers')
    candidates = []
    for cid in ids:
        info, = json.loads(r.run(['docker', 'inspect', cid]))
        image = info['Config']['Image'].split('@')[0].split(':')[0].lower()
        if image not in ['pasarguard/node', 'ghcr.io/pasarguard/node'] or info['HostConfig']['NetworkMode'] != 'host':
            continue
        top = r.run(['docker', 'top', cid, '-eo', 'pid,comm'])
        pids = [int(line.split()[0]) for line in top.splitlines()[1:] if line.split()[-1] == 'xray']
        if len(pids) != 1:
            continue
        proc = r.process(pids[0])
        r.require(proc['parent'] == info['State']['Pid'], 'core_parent_changed')
        sha = r.file_digest('/proc/'+str(proc['pid'])+'/exe')
        candidates.append(dict(node_container=info['Name'].lstrip('/'), node_image_id=info['Image'],
                               xray_sha256=sha, pid=proc['pid'], start_ticks=proc['start_ticks']))
    r.require(len(candidates) == 1, 'ambiguous_or_missing_pasarguard_node')
    return candidates[0]


def approved_profile(plan, ident, inv, cfg):
    r.require(plan.get('schema') == 1 and plan.get('monitor_id') == ident['node_id']
              and time.time() < plan.get('expires', 0) < time.time()+900, 'invalid_plan')
    p = plan['profile']
    # Construct a fixed schema; no shell arguments, paths, or arbitrary endpoints
    # from a plan may reach the root worker.
    keys = ['node_name', 'node_address', 'node_id', 'core_config_id', 'node_container',
            'node_image_id', 'xray_sha256', 'probe_inbound', 'https_host']
    out = {k: p[k] for k in keys}
    for key in ['node_container', 'node_image_id', 'xray_sha256']:
        r.require(out[key] == inv[key], 'runtime_not_approved')
    r.require(re.fullmatch('[a-f0-9]{64}', out['xray_sha256']), 'invalid_core_digest')
    r.require(isinstance(out['node_id'], int) and isinstance(out['core_config_id'], int), 'invalid_node_id')
    out['node_address'] = str(ipaddress.IPv4Address(out['node_address']))
    r.require(re.fullmatch(r'[A-Za-z0-9.-]{1,253}', out['https_host']), 'invalid_tls_name')
    out['probe_destination'] = peer_host(cfg)[0]
    out['monitor_transport'] = True
    out['fleet_source'] = plan['source']
    r.validate_policy(out['fleet_source'], out)
    return out


def agent(cfg):
    import grp
    import fcntl
    LOCAL.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    now = time.time()
    local = json.loads(LOCAL.read_text()) if LOCAL.exists() else {}
    if local.get('needs_report'):
        try:
            report = {k: local[k] for k in ['status', 'reason', 'inventory', 'proof'] if k in local}
            exchange(cfg, json.loads(IDENTITY.read_text()), report)
            local.pop('needs_report', None)
            local['report_after'] = now+300
            r.atomic_json(LOCAL, local)
        except Exception:
            # Status reporting cannot bypass the proof's five-minute backoff.
            return
    binding = json.loads(r.BINDING.read_text()) if r.BINDING.exists() else None
    state = json.loads(r.STATE.read_text()) if r.STATE.exists() else {}
    boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
    bound = binding is not None and r.still_bound(binding, state, boot)
    if bound and local.get('report_after', 0) > now:
        return
    if local.get('retry_after', 0) > now:
        return
    local['retry_after'] = now+300
    r.atomic_json(LOCAL, local)
    try:
        ident = json.loads(IDENTITY.read_text())
        inv = local.get('inventory') if bound else None
        if not inv:
            inv = inventory()
        inv['addresses'] = local_addresses()
        local['inventory'] = inv
        report = {'status': 'ready' if bound else local.get('status', 'discovered'), 'inventory': inv}
        if local.get('reason'): report['reason'] = local['reason']
        if local.get('proof'): report['proof'] = local['proof']
        plan = exchange(cfg, ident, report)
        if plan and plan.get('core_stage'):
            from presence_core_stage import stage
            local.setdefault('proof', {})['core_stage'] = stage(plan, cfg, ident, inv)
            local['needs_report'] = True
        if bound:
            local.update(status='ready', report_after=now+300, retry_after=0)
        elif plan is None:
            local.update(status='pending', retry_after=now+60)
        else:
            profile = approved_profile(plan, ident, inv, cfg)
            # Share the legacy recovery lock so a previous pilot timer cannot
            # publish a competing binding while automatic enrollment runs.
            with open('/run/marzwatch-presence-recovery.lock', 'w') as lock:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                original = r.BINDING.read_bytes() if r.BINDING.exists() else None
                seed = binding or {'schema': 1, 'namespace': '', 'cores': [{}]}
                candidate = r.recover(profile, seed, boot)
                r.require((r.BINDING.read_bytes() if r.BINDING.exists() else None) == original, 'binding_changed_concurrently')
                r.atomic_json(PROFILE, profile)
                r.atomic_json(r.BINDING, candidate, mode=0o640, gid=grp.getgrnam('marzwatch').gr_gid)
                r.atomic_json(r.STATE, {'boot_id': boot, 'binding_digest': r.digest(candidate)})
            method = candidate.get('verification_method', 'existing_account_flow')
            proof = dict(local.get('proof', {}), runtime_tracking_verified=True,
                         verification_method=method, vpn_restarted=False,
                         pid=candidate['cores'][0]['pid'],
                         existing_account_flow=method == 'existing_account_flow',
                         tls_verified=method == 'existing_account_flow')
            local.update(status='ready', reason='', retry_after=0, report_after=0,
                         needs_report=True,
                         proof=proof)
    except Exception as e:
        local.update(status='unverified', needs_report=True,
                     reason=str(e) if isinstance(e, r.Unverified) else type(e).__name__)
    r.atomic_json(LOCAL, local)
    print(json.dumps({k: local[k] for k in ['status', 'reason'] if k in local}))


FLEET_READ = r'''
import asyncio,json,sys
from sqlalchemy import select,text
from app.db import GetDB
from app.db.base import engine
from app.db.models import Node,CoreConfig,User
async def main():
 args=json.load(sys.stdin)
 try:
  async with GetDB() as db:
   await db.execute(text('SET TRANSACTION READ ONLY'))
   await db.execute(text("SET LOCAL statement_timeout='2000ms'"))
   rows=(await db.execute(select(Node.id,Node.name,Node.address,Node.core_config_id,CoreConfig.config).join(CoreConfig,Node.core_config_id==CoreConfig.id).where(Node.id.in_(args['ids'])))).all()
   ids=(await db.execute(select(User.id).where(User.status=='active').order_by(User.id).limit(5))).scalars().all()
   print(json.dumps({str(x.id):{'name':x.name,'address':x.address,'core_id':x.core_config_id,'config':x.config,'candidates':ids} for x in rows}))
 finally:await engine.dispose()
try:asyncio.run(asyncio.wait_for(main(),7))
except Exception:sys.exit(1)
'''


def refresh(force=False):
    if not FLEET.exists(): return
    if not force and PLANS.exists() and time.time()-PLANS.stat().st_mtime < 300: return
    cfg = root_json(FLEET)
    entries = cfg['nodes']
    r.require(len(entries) <= 100, 'too_many_enrolled_nodes')
    sources = json.loads(r.run(['docker', 'exec', '-i', cfg['panel_container'], '/code/.venv/bin/python', '-c', FLEET_READ],
                              timeout=10, input=json.dumps({'ids': [p['node_id'] for p in entries.values()]})))
    publish_plans(cfg, sources)


def publish_plans(cfg, sources):
    entries = cfg['nodes']
    namespace = root_json(r.BINDING)['namespace']
    expires = int(time.time())+660
    plans = {}
    for monitor_id, profile in entries.items():
        if not profile.get('enabled'): continue
        try:
            source = minimal_source(sources[str(profile['node_id'])], profile['probe_inbound'])
            r.validate_policy(source, profile)
            source['namespace'] = namespace
            plans[monitor_id] = dict(schema=1, monitor_id=monitor_id, expires=expires, profile=profile, source=source)
            if monitor_id in cfg.get('core_stages', {}):
                plans[monitor_id]['core_stage'] = cfg['core_stages'][monitor_id]
        except (KeyError, r.Unverified):
            continue
    import grp
    r.atomic_json(PLANS, {'expires': expires, 'plans': plans}, mode=0o640, gid=grp.getgrnam('marzwatch').gr_gid)


def main():
    import fcntl
    import sys
    r.require(os.geteuid() == 0, 'root_required')
    with open('/run/marzwatch-presence-enroll.lock', 'w') as lock:
        try: fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError: return
        cfg = root_json(CONFIG)
        if cfg['role'] == 'central':
            if cfg.get('presence_auto_enroll') and FLEET.exists():
                from presence_auto import tick
                tick(cfg, '--refresh' in sys.argv)
            else:
                refresh('--refresh' in sys.argv)
        elif cfg['role'] == 'agent': agent(cfg)


if __name__ == '__main__':
    try: main()
    except Exception as e:
        print(json.dumps({'status': 'unverified', 'reason': str(e) if isinstance(e, r.Unverified) else type(e).__name__}))
        raise SystemExit(1)
