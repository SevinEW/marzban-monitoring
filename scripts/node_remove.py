#!/usr/bin/env python3
"""Fixed MarzWatch-only cleanup, authorized by a fresh per-node Central action.

No path, command, package name or service name is accepted from the network.
PasarGuard, Xray, Docker, firewall, host swap and other projects are untouched.
"""
import hashlib
import hmac
import http.client
import json
import os
from pathlib import Path
import re
import shutil
import ssl
import stat
import subprocess
import time

REQUEST = Path('/var/lib/marzwatch/remove.request')
CONFIG = Path('/etc/marzwatch/config.json')
IDENTITY = Path('/var/lib/marzwatch/identity.json')
UNITS = ('marzwatch.service', 'marzwatch-auto-update.timer', 'marzwatch-auto-update.service',
         'marzwatch-presence-enroll.timer', 'marzwatch-presence-enroll.service',
         'marzwatch-presence-recovery.timer', 'marzwatch-presence-recovery.service',
         'marzwatch-pg-meter.service')
FILES = ('/usr/local/bin/marzwatch', '/usr/local/bin/marzwatchctl',
         '/usr/local/bin/marzwatch.new', '/usr/local/lib/marzwatch',
         '/var/lib/marzwatch-updater', '/etc/marzwatch', '/var/lib/marzwatch')


def require(ok, message):
    if not ok: raise RuntimeError(message)


def request_nonce():
    try:
        # The signal directory is writable by the monitor. Never follow a link.
        fd = os.open(REQUEST, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as f:
            require(stat.S_ISREG(os.fstat(f.fileno()).st_mode), 'invalid signal')
            value = f.read(65).decode('ascii')
        return value if re.fullmatch(r'[A-Za-z0-9_-]{32}', value) else None
    except (OSError, UnicodeError, RuntimeError): return None


def maybe_start():
    if not request_nonce(): return False
    subprocess.run(['systemctl', 'start', '--no-block', 'marzwatch-uninstall.service'],
                   check=True, timeout=8, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    return True


def exchange(cfg, ident, nonce, phase):
    # Reuse URL validation only; TLS must be verified before transmitting auth.
    from presence_fleet import peer_host
    host, port = peer_host(cfg)
    pin = cfg.get('cert_fingerprint', '').lower()
    require(re.fullmatch(r'[a-f0-9]{64}', pin), 'invalid certificate pin')
    body = json.dumps({'nonce': nonce, 'phase': phase}).encode()
    stamp = str(int(time.time()))
    signature = hmac.new(ident['node_secret'].encode(), b'node-remove-v1\n' + stamp.encode() + b'\n' + body, hashlib.sha256).hexdigest()
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    conn = http.client.HTTPSConnection(host, port, timeout=8, context=context)
    try:
        conn.connect()
        require(hmac.compare_digest(pin, hashlib.sha256(conn.sock.getpeercert(binary_form=True)).hexdigest()), 'certificate mismatch')
        conn.request('POST', '/api/v1/node-action', body, headers={
            'Content-Type': 'application/json', 'X-MW-Node': ident['node_id'],
            'X-MW-Time': stamp, 'X-MW-Signature': signature})
        response = conn.getresponse()
        raw = response.read(4097)
        require(len(raw) <= 4096, 'oversized action')
        if phase == 'complete' and response.status == 410: return {'status': 'removed'}
        require(response.status == 200, 'action unavailable')
        plan = json.loads(raw)
        require(plan.get('action') == 'uninstall-marzwatch' and plan.get('node_id') == ident['node_id']
                and plan.get('nonce') == nonce, 'action identity mismatch')
        if phase in ('poll', 'accept'):
            require(plan.get('status') == ('pending' if phase == 'poll' else 'accepted')
                    and time.time() <= plan.get('expires', 0) <= time.time()+305, 'expired action')
        return plan
    finally: conn.close()


def targets():
    return [Path(p) for p in FILES] + [Path('/etc/systemd/system') / (u+s)
            for u in (*UNITS, 'marzwatch-uninstall.service') for s in ('', '.d')]


def preflight(paths):
    for p in paths:
        require(str(p).startswith(('/etc/marzwatch', '/var/lib/marzwatch', '/usr/local/', '/etc/systemd/system/marzwatch')), 'invalid cleanup scope')
        require(p.parent.resolve() == p.parent, 'redirected parent directory')
        if p.is_symlink() or not p.exists(): continue
        require(not p.is_mount(), 'mounted project directory')
        if p.is_dir():
            count = 0
            for directory, dirs, files in os.walk(p, followlinks=False):
                count += len(dirs)+len(files)
                require(count <= 20000, 'unexpected project size')
                for child in dirs:
                    require(not (Path(directory)/child).is_mount(), 'nested mount in project')


def cleanup(paths):
    # Called only while holding the root updater lock and after signed accept.
    subprocess.run(['systemctl', 'disable', '--now', *UNITS], timeout=35,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for unit in UNITS:
        status = subprocess.run(['systemctl', 'is-active', unit], capture_output=True, text=True, timeout=4).stdout.strip()
        require(status in ('inactive', 'failed', 'unknown', ''), 'monitoring service still active')
    for p in paths:
        if p.is_symlink(): p.unlink()
        elif p.is_dir(): shutil.rmtree(p)
        elif p.exists(): p.unlink()
    subprocess.run(['systemctl', 'daemon-reload'], check=True, timeout=10,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    # Remove only our service account, never its unrelated group members.
    import pwd, grp
    try:
        user = pwd.getpwnam('marzwatch')
        require(user.pw_dir in ('/nonexistent', '/var/lib/marzwatch', '/'), 'unexpected monitoring account')
        subprocess.run(['userdel', 'marzwatch'], check=True, timeout=8, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except KeyError: pass
    try:
        group = grp.getgrnam('marzwatch')
        if not group.gr_mem: subprocess.run(['groupdel', 'marzwatch'], timeout=8, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except KeyError: pass
    require(not any(p.exists() or p.is_symlink() for p in paths), 'project files remain')


def run():
    require(os.geteuid() == 0, 'root required')
    from presence_fleet import root_json
    cfg = root_json(CONFIG)
    require(cfg.get('role') == 'agent', 'Central is protected')
    nonce = request_nonce()
    if not nonce: return
    ident = json.loads(IDENTITY.read_text())
    require(ident.get('node_id') != 'central' and re.fullmatch(r'[A-Za-z0-9_-]{1,80}', ident.get('node_id', '')), 'invalid node')
    import fcntl
    with open('/run/marzwatch-auto-update.lock', 'a') as lock:
        try: fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError: return  # retry next lightweight timer tick
        try:
            exchange(cfg, ident, nonce, 'poll')
            paths = targets()
            preflight(paths)
            exchange(cfg, ident, nonce, 'accept')
            cleanup(paths)
        except Exception:
            try: exchange(cfg, ident, nonce, 'failed')
            except Exception: pass
            # Never erase a newer signal that arrived during this operation.
            if request_nonce() == nonce: REQUEST.unlink(missing_ok=True)
            raise
        # Credentials are retained only in this process after their files vanish.
        # A failed final acknowledgement leaves an explicit incomplete state.
        for attempt in range(3):
            try:
                exchange(cfg, ident, nonce, 'complete')
                print('MarzWatch removal confirmed; other services were not changed.')
                return
            except Exception:
                if attempt == 2: raise RuntimeError('cleanup finished; Central acknowledgement unavailable') from None
                time.sleep(2)


if __name__ == '__main__':
    try: run()
    except Exception as error:
        print('MarzWatch removal incomplete: ' + type(error).__name__)
        raise SystemExit(1)
