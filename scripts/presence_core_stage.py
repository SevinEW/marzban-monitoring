"""Explicit, hash-pinned staging of the reviewed v26.10.21 presence correction.

No restart, shell, arbitrary URL or executable path from a remote plan. The
operator starts the one approved node separately after inspecting this report.
"""
import hashlib
import hmac
import http.client
import json
import os
from pathlib import Path, PurePosixPath
import ssl
import tempfile
import time
import recover_pasarguard_presence as r

KIND = 'xray-loopback-v26.10.21-amd64'
SHA = '18fad6a566313b657f1d862c6525b89e69f751ae1d9070ab6720190647af6b27'
PREVIOUS_SHA = 'a27e3f95ab6ce270956d0cecd32eda4686a811afc71c64a6a734a3dede14883e'
ASSET = 'xray-online-linux-amd64'
LIMIT = 32 * 1024 * 1024
STATE = Path('/var/lib/marzwatch-updater/core-stage.json')


def download(cfg, ident, target):
    from presence_fleet import peer_host
    host, port = peer_host(cfg)
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    ctx.minimum_version = ssl.TLSVersion.TLSv1_2
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    conn = http.client.HTTPSConnection(host, port, timeout=12, context=ctx)
    try:
        conn.connect()
        actual = hashlib.sha256(conn.sock.getpeercert(binary_form=True)).hexdigest()
        r.require(hmac.compare_digest(actual, cfg['cert_fingerprint'].lower()), 'central_certificate_mismatch')
        stamp = str(int(time.time()))
        body = ASSET.encode()
        signature = hmac.new(ident['node_secret'].encode(), b'release-v1\n'+stamp.encode()+b'\n'+body, hashlib.sha256).hexdigest()
        conn.request('POST', '/api/v1/release', body=body, headers={
            'X-MW-Node': ident['node_id'], 'X-MW-Time': stamp, 'X-MW-Signature': signature})
        response = conn.getresponse()
        r.require(response.status == 200, 'core_asset_unavailable')
        total = 0
        with target.open('wb') as output:
            while True:
                block = response.read(65536)
                if not block: break
                total += len(block)
                r.require(total <= LIMIT, 'core_asset_too_large')
                output.write(block)
        r.require(r.file_digest(target) == SHA, 'core_asset_checksum_mismatch')
    finally:
        conn.close()


def stage(plan, cfg, ident, inv):
    directive = plan.get('core_stage')
    if directive is None:
        return None
    r.require(plan.get('monitor_id') == ident['node_id'] and time.time() < plan.get('expires', 0)
              and directive.get('kind') == KIND and directive.get('sha256') == SHA
              and directive.get('previous_sha256') == PREVIOUS_SHA,
              'core_stage_not_approved')
    profile = plan['profile']
    r.require(inv['node_container'] == profile['node_container']
              and inv['node_image_id'] == profile['node_image_id'], 'core_stage_runtime_changed')
    state = json.loads(STATE.read_text()) if STATE.exists() else {}
    if inv['xray_sha256'] == SHA:
        r.require(state.get('sha256') == SHA, 'unexpected_corrected_core')
        return dict(status='active', sha256=SHA, previous_sha256=state['previous_sha256'])
    r.require(inv['xray_sha256'] == directive.get('previous_sha256'), 'core_stage_version_changed')
    if state.get('sha256') == SHA and state.get('status') == 'staged':
        # A recreated container may have silently restored the stock binary.
        # Never interpret an old staging receipt as proof of the current file.
        r.require(state.get('node_container') == inv['node_container']
                  and r.run(['docker', 'exec', inv['node_container'], 'sha256sum',
                             state['executable']]).split()[0] == SHA, 'staged_core_replaced')
        return {k: state[k] for k in ['status', 'sha256', 'previous_sha256']}
    r.require(not state, 'core_stage_needs_review')
    container = inv['node_container']
    info, = json.loads(r.run(['docker', 'inspect', container]))
    r.require(info['State']['Running'] and info['Image'] == inv['node_image_id'], 'core_stage_container_changed')
    env = dict(v.split('=', 1) for v in info['Config'].get('Env', []) if '=' in v)
    path = env.get('XRAY_EXECUTABLE_PATH', '/usr/local/bin/xray')
    target = PurePosixPath(path)
    r.require(target.is_absolute() and target.name == 'xray' and '..' not in target.parts,
              'unsupported_core_path')
    # An individual bind-mounted executable cannot be atomically renamed.
    # Never truncate a running executable or remount it to get around this.
    r.require(not any(m['Destination'] == path for m in info.get('Mounts', [])), 'core_file_bind_mount')
    proc = r.process(inv['pid'])
    r.require(proc['start_ticks'] == inv['start_ticks'] and proc['parent'] == info['State']['Pid'], 'core_stage_process_changed')
    exe = '/proc/'+str(inv['pid'])+'/exe'
    r.require(r.file_digest(exe) == directive['previous_sha256'], 'core_stage_executable_changed')
    disk_sha = r.run(['docker', 'exec', container, 'sha256sum', path]).split()[0]
    r.require(disk_sha == directive['previous_sha256'], 'core_disk_changed')
    backup = STATE.parent/'core-backups'/directive['previous_sha256']
    backup.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    if not backup.exists():
        r.run(['docker', 'cp', container+':'+path, str(backup)], timeout=15)
        backup.chmod(0o600)
    r.require(r.file_digest(backup) == directive['previous_sha256'], 'core_backup_mismatch')
    staged = str(target.parent/('.marzwatch-xray-'+SHA[:16]))
    with tempfile.TemporaryDirectory(prefix='mw-core-stage-') as tmp:
        binary = Path(tmp)/'xray'
        download(cfg, ident, binary)
        r.run(['docker', 'cp', str(binary), container+':'+staged], timeout=15)
        r.run(['docker', 'exec', container, 'chmod', '0755', staged])
        r.require(r.run(['docker', 'exec', container, 'sha256sum', staged]).split()[0] == SHA, 'staged_core_mismatch')
        version = r.run(['docker', 'exec', container, staged, 'version'])
        r.require('Xray 26.10.21 ' in version and 'linux/amd64' in version, 'staged_core_wrong_platform')
        # Only this already-running binary version is supported; compare before
        # replacing on disk. Existing connections keep their current process.
        r.require(r.process(inv['pid']) == proc and r.file_digest(exe) == directive['previous_sha256'], 'core_changed_during_stage')
        state = dict(status='installing', sha256=SHA, previous_sha256=directive['previous_sha256'],
                     node_container=container, executable=path, backup=str(backup), pid=inv['pid'])
        r.atomic_json(STATE, state)
        r.run(['docker', 'exec', container, 'mv', '-f', staged, path])
        r.require(r.run(['docker', 'exec', container, 'sha256sum', path]).split()[0] == SHA, 'installed_core_mismatch')
        state['status'] = 'staged'
        r.atomic_json(STATE, state)
    return {k: state[k] for k in ['status', 'sha256', 'previous_sha256']}
