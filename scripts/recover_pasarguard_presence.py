#!/usr/bin/env python3
"""Opt-in recovery for an already-enabled, single-level PasarGuard pilot.

No VPN restart, policy change, user creation, counter reset or SQL write. The
normal path only reads local process/binding metadata. A changed generation is
verified with a bounded read-only config query and one local existing-user flow.
Unknown images, policies, sources and unsuccessful probes fail closed.
"""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import socket
import ssl
import subprocess
import tempfile
import time

BINDING = Path('/etc/marzwatch/presence.json')
STATE = Path('/var/lib/marzwatch-updater/presence-recovery.json')
MONITOR = '/usr/local/bin/marzwatch'


class Unverified(Exception):
    pass


def require(ok, reason):
    if not ok:
        raise Unverified(reason)


def run(args, timeout=5, input=None):
    p = subprocess.run(args, input=input, capture_output=True, text=True, timeout=timeout)
    label = 'xray_'+args[2] if len(args) > 2 and args[1] == 'api' else Path(args[0]).name+'_'+args[1]
    require(p.returncode == 0, 'source_command_failed:'+label)
    require(len(p.stdout) <= 4 * 1024 * 1024, 'source_response_too_large')
    return p.stdout


def process(pid):
    root = Path('/proc') / str(pid)
    fields = (root/'stat').read_text().rsplit(')', 1)[1].split()
    require(fields[0] != 'Z', 'core_stopped')
    return {'pid': pid, 'start_ticks': fields[19], 'parent': int(fields[1])}


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()


def still_bound(binding, state, boot, read_process=process):
    try:
        core, = binding['cores']
        return (binding.get('tracking_verified') is True and binding.get('boot_id') == boot
                and state.get('boot_id') == boot and state.get('binding_digest') == digest(binding)
                and read_process(core['pid'])['start_ticks'] == core['start_ticks'])
    except (OSError, ValueError, KeyError, TypeError, Unverified):
        return False


def atomic_json(path, value, mode=0o600, gid=0):
    fd, name = tempfile.mkstemp(prefix='.presence-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as f:
            json.dump(value, f)
            f.flush()
            os.fsync(f.fileno())
        os.chown(name, 0, gid)
        os.chmod(name, mode)
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def listeners(pid):
    root = Path('/proc') / str(pid)
    owned = set()
    for fd in (root/'fd').iterdir():
        try:
            link = os.readlink(fd)
        except FileNotFoundError:
            continue
        if link.startswith('socket:['):
            owned.add(link[8:-1])
    found = set()
    for family in ['tcp', 'tcp6']:
        for line in (root/'net'/family).read_text().splitlines()[1:]:
            f = line.split()
            if f[3] == '0A' and f[9] in owned:
                host, port = f[1].split(':')
                raw = bytes.fromhex(host)
                raw = b''.join(raw[i:i+4][::-1] for i in range(0, len(raw), 4))
                found.add((str(ipaddress.ip_address(raw)), int(port, 16)))
    return found


PANEL_READ = r'''
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
   row=(await db.execute(select(Node.name,Node.address,Node.core_config_id,CoreConfig.config).join(CoreConfig,Node.core_config_id==CoreConfig.id).where(Node.id==args['node_id']))).one()
   ids=(await db.execute(select(User.id).where(User.status=='active').order_by(User.id).limit(5))).scalars().all()
   print(json.dumps({'name':row.name,'address':row.address,'core_id':row.core_config_id,'config':row.config,'candidates':ids}))
 finally:await engine.dispose()
try:asyncio.run(asyncio.wait_for(main(),7))
except Exception:sys.exit(1)
'''


def validate_policy(source, profile):
    require(source['name'] == profile['node_name'] and source['address'] == profile['node_address']
            and source['core_id'] == profile['core_config_id'], 'pilot_assignment_changed')
    cfg = source['config']
    levels = cfg.get('policy', {}).get('levels', {})
    # The pinned PasarGuard Node serializes panel users with level zero. Other
    # levels require their own runtime proof; never extrapolate this pilot.
    require(set(levels) == {'0'} and levels['0'].get('statsUserOnline') is True,
            'online_policy_unverified')
    for inbound in cfg.get('inbounds', []):
        for client in inbound.get('settings', {}).get('clients', []):
            require(client.get('level', 0) == 0, 'unsupported_user_level')
    probes = [i for i in cfg.get('inbounds', []) if i.get('tag') == profile['probe_inbound']]
    require(len(probes) == 1, 'probe_inbound_missing')
    i = probes[0]
    require(i.get('protocol') == 'vless' and i.get('streamSettings', {}).get('network') == 'ws'
            and i.get('streamSettings', {}).get('security', 'none') == 'none'
            and isinstance(i.get('port'), int), 'unsupported_probe_transport')
    require(i['streamSettings'].get('wsSettings', {}).get('host', '') == '', 'unsupported_probe_host')
    return i


def read_source(profile):
    peer = profile.get('panel_ssh')
    if peer:
        # This dedicated key is restricted by Central's authorized_keys to one
        # read-only source command for this exact node. No remote shell is used.
        host = str(ipaddress.IPv4Address(peer['host']))
        return json.loads(run(['ssh', '-F', '/dev/null', '-n', '-T',
                               '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=4',
                               '-o', 'StrictHostKeyChecking=yes', '-o', 'IdentitiesOnly=yes',
                               '-o', 'UserKnownHostsFile='+peer['known_hosts_file'],
                               '-i', peer['identity_file'], 'root@'+host], timeout=14))
    return json.loads(run(['docker', 'exec', '-i', profile['panel_container'], '/code/.venv/bin/python', '-c', PANEL_READ],
                          timeout=10, input=json.dumps({'node_id': profile['node_id']})))


def probe_destination(profile):
    target = str(ipaddress.IPv4Address(profile.get('probe_destination', profile['node_address'])))
    require(target == profile['node_address'] or target == profile.get('panel_ssh', {}).get('host'),
            'probe_destination_not_authorized')
    return target


def candidate_binding(old, boot, proc, address):
    # A bootstrap may be explicitly unverified: only the runtime proof below
    # can turn it into a binding accepted by the collector.
    require(old.get('schema') == 1
            and 16 <= len(old.get('namespace', '')) <= 128 and len(old.get('cores', [])) == 1,
            'pilot_binding_missing')
    return dict(schema=1, boot_id=boot, namespace=old['namespace'], tracking_verified=True,
                cores=[dict(address=address, pid=proc['pid'], start_ticks=proc['start_ticks'])])


def discover_api(old, boot, proc, path, ports, excluded):
    candidates = sorted(p for h, p in ports if h == '127.0.0.1' and p not in excluded)
    require(0 < len(candidates) <= 4, 'ambiguous_local_api')
    found = []
    for port in candidates:
        b = candidate_binding(old, boot, proc, '127.0.0.1:' + str(port))
        path.write_text(json.dumps(b)); path.chmod(0o600)
        result = json.loads(run([MONITOR, 'presence-check', '--binding', str(path)], timeout=3))
        if result.get('status') == 'ok':
            found.append(b)
    require(len(found) == 1, 'stats_api_unverified')
    return found[0]


def exact(sock, size):
    data = b''
    while len(data) < size:
        block = sock.recv(size-len(data))
        require(bool(block), 'probe_connection_ended')
        data += block
    return data


def prove_tracking(exe, binding, source, inbound, profile):
    api = binding['cores'][0]['address']
    user = None
    for uid in source['candidates']:
        try:
            obj = json.loads(run([exe, 'api', 'inbounduser', '--server='+api,
                                  '-tag='+inbound['tag'], '-email='+str(uid)], timeout=3))
        except (Unverified, subprocess.TimeoutExpired):
            continue
        if obj.get('users'):
            user = obj['users'][0]
            require(user.get('email') == str(uid) and user.get('level', 0) == 0,
                    'probe_account_mismatch')
            break
    require(user is not None, 'no_existing_probe_account')
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0)); port = s.getsockname()[1]
    cfg = {'log': {'loglevel': 'none'},
           'inbounds': [{'listen': '127.0.0.1', 'port': port, 'protocol': 'socks', 'settings': {'auth': 'noauth', 'udp': False}}],
           'outbounds': [{'protocol': 'vless', 'sendThrough': '127.0.0.2',
                          'settings': {'vnext': [{'address': '127.0.0.1', 'port': inbound['port'],
                                                 'users': [{'id': user['account']['id'], 'encryption': 'none'}]}]},
                          'streamSettings': inbound['streamSettings']}]}
    client = None
    flow = None
    try:
        client = subprocess.Popen(['nice', '-n', '15', exe, 'run', '-c', 'stdin:'],
                                  stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                  env=dict(os.environ, GOMAXPROCS='1', GOMEMLIMIT='32MiB'))
        client.stdin.write(json.dumps(cfg).encode()); client.stdin.close()
        for _ in range(25):
            require(client.poll() is None, 'probe_client_failed')
            try:
                flow = socket.create_connection(('127.0.0.1', port), .2)
                break
            except OSError: time.sleep(.1)
        require(flow is not None, 'probe_client_unavailable')
        flow.settimeout(3)
        flow.sendall(b'\x05\x01\x00')
        require(exact(flow, 2) == b'\x05\x00', 'probe_handshake_failed')
        flow.sendall(b'\x05\x01\x00\x01'+socket.inet_aton(probe_destination(profile))+(443).to_bytes(2, 'big'))
        reply = exact(flow, 4)
        require(reply[:2] == b'\x05\x00' and reply[3] in [1, 4], 'probe_destination_failed')
        exact(flow, 6 if reply[3] == 1 else 18)
        flow = ssl.create_default_context().wrap_socket(flow, server_hostname=profile['https_host'])
        # Unlike an empty bulk response, a live tracked flow proves that the
        # running core has online tracking enabled. Raw account IDs never leave.
        tracked = json.loads(run([exe, 'api', 'statsonline', '--server='+api,
                                  '-email='+user['email']], timeout=3))
        require(int(tracked.get('stat', {}).get('value', 0)) > 0, 'runtime_tracking_unverified')
    finally:
        if flow is not None: flow.close()
        if client is not None and client.poll() is None:
            client.terminate()
            try: client.wait(timeout=2)
            except subprocess.TimeoutExpired: client.kill(); client.wait(timeout=2)


def recover(profile, binding, boot):
    info, = json.loads(run(['docker', 'inspect', profile['node_container']]))
    require(info['State']['Running'] and info['HostConfig']['NetworkMode'] == 'host'
            and info['Image'] == profile['node_image_id'], 'node_runtime_changed')
    top = run(['docker', 'top', profile['node_container'], '-eo', 'pid,comm'])
    pids = [int(line.split()[0]) for line in top.splitlines()[1:] if line.split()[-1] == 'xray']
    require(len(pids) == 1, 'ambiguous_running_core')
    proc = process(pids[0])
    require(proc['parent'] == info['State']['Pid'], 'core_parent_changed')
    exe = '/proc/'+str(proc['pid'])+'/exe'
    with open(exe, 'rb') as f:
        require(hashlib.file_digest(f, 'sha256').hexdigest() == profile['xray_sha256'], 'core_version_changed')
    # Execute inside the existing image only after a process change. No steady
    # state panel imports, database connections or Docker calls are made.
    source = read_source(profile)
    if profile.get('panel_ssh'):
        namespace = source.get('namespace')
        require(isinstance(namespace, str) and 16 <= len(namespace) <= 128, 'invalid_source_namespace')
        binding = dict(binding, namespace=namespace)
    inbound = validate_policy(source, profile)
    owned = listeners(proc['pid'])
    require(any((host, inbound['port']) in owned for host in ['0.0.0.0', '127.0.0.1', '::']),
            'probe_socket_owner_mismatch')
    # The node identity must be local. TLS testing terminates either locally or
    # at the explicitly configured Central, never an arbitrary external site.
    addresses = json.loads(run(['ip', '-j', '-4', 'addr', 'show']))
    require(profile['node_address'] in {a['local'] for i in addresses for a in i.get('addr_info', [])},
            'probe_destination_not_local')
    probe_destination(profile)
    excluded = {i['port'] for i in source['config'].get('inbounds', []) if isinstance(i.get('port'), int)}
    with tempfile.TemporaryDirectory(prefix='marzwatch-presence-recovery-') as tmp:
        candidate = discover_api(binding, boot, proc, Path(tmp)/'binding.json', owned, excluded)
        prove_tracking(exe, candidate, source, inbound, profile)
    require(process(proc['pid']) == proc and Path('/proc/sys/kernel/random/boot_id').read_text().strip() == boot,
            'core_changed_during_probe')
    require(candidate['cores'][0]['address'].rsplit(':', 1)[1].isdigit(), 'invalid_api')
    require(('127.0.0.1', int(candidate['cores'][0]['address'].rsplit(':', 1)[1])) in listeners(proc['pid']),
            'api_socket_owner_changed')
    return candidate


def main():
    import fcntl
    import grp
    p = argparse.ArgumentParser()
    p.add_argument('--profile', default='/etc/marzwatch/presence-recovery.json')
    p.add_argument('--verify', action='store_true', help='repeat runtime proof, even if already bound')
    p.add_argument('--initialize', action='store_true', help='create a first binding only after a successful runtime proof')
    args = p.parse_args()
    require(os.geteuid() == 0, 'root_required')
    profile_path = Path(args.profile)
    require(profile_path.stat().st_uid == 0 and profile_path.stat().st_mode & 0o022 == 0, 'unsafe_profile')
    profile = json.loads(profile_path.read_text())
    original = BINDING.read_bytes() if BINDING.exists() else None
    require(original is not None or (args.initialize and profile.get('panel_ssh')), 'binding_missing')
    binding = json.loads(original) if original is not None else {'schema': 1, 'namespace': '', 'cores': [{}]}
    boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
    STATE.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    with open('/run/marzwatch-presence-recovery.lock', 'w') as lock:
        try: fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError: return
        state = json.loads(STATE.read_text()) if STATE.exists() else {}
        if not args.verify and still_bound(binding, state, boot):
            return
        if not args.verify and state.get('boot_id') == boot and state.get('retry_after', 0) > time.time():
            return
        # At most one expensive retry per five minutes, even if a dependency is
        # down or this process is killed mid-proof. Normal checks never write.
        atomic_json(STATE, {'boot_id': boot, 'retry_after': time.time()+300})
        candidate = recover(profile, binding, boot)
        require((BINDING.read_bytes() if BINDING.exists() else None) == original, 'binding_changed_concurrently')
        atomic_json(BINDING, candidate, mode=0o640, gid=grp.getgrnam('marzwatch').gr_gid)
        atomic_json(STATE, {'boot_id': boot, 'binding_digest': digest(candidate)})
        print(json.dumps({'status': 'rebound', 'pid': candidate['cores'][0]['pid'], 'vpn_restarted': False}))


if __name__ == '__main__':
    try: main()
    except Exception as e:
        # Never log exception text from APIs, subprocesses or the database.
        print(json.dumps({'status': 'unverified', 'reason': str(e) if isinstance(e, Unverified) else type(e).__name__}))
        raise SystemExit(1)
