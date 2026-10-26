#!/usr/bin/env python3
"""Dedicated forced-command reader for one authorized node's recovery proof.

Install only on Central. A per-node SSH key must use restrict and a fixed command
containing its node ID. This exports no credentials, client UUIDs or full config.
"""
import json
import os
from pathlib import Path
import sys
from recover_pasarguard_presence import PANEL_READ, run, require


def source_slot(directory='/run'):
    import fcntl
    for i in range(2):
        lock = open(Path(directory)/('marzwatch-presence-source-'+str(i)+'.lock'), 'w')
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            return lock
        except BlockingIOError:
            lock.close()
    require(False, 'source_busy')


def minimal_source(source, probe_tag):
    cfg = source['config']
    out = {k: source[k] for k in ['name', 'address', 'core_id']}
    out['candidates'] = [int(x) for x in source['candidates'][:5]]
    inbounds = []
    for i in cfg.get('inbounds', []):
        row = {k: i.get(k) for k in ['tag', 'port', 'protocol']}
        row['settings'] = {'clients': [{'level': c.get('level', 0)} for c in i.get('settings', {}).get('clients', [])]}
        if i.get('tag') == probe_tag:
            stream = i.get('streamSettings', {})
            row['streamSettings'] = {k: stream[k] for k in ['network', 'security', 'wsSettings'] if k in stream}
        inbounds.append(row)
    out['config'] = {'policy': {'levels': {k: {'statsUserOnline': v.get('statsUserOnline', False)}
                                          for k, v in cfg.get('policy', {}).get('levels', {}).items()}},
                     'inbounds': inbounds}
    return out


def main():
    require(os.geteuid() == 0 and len(sys.argv) == 2, 'invalid_source_request')
    require(not os.environ.get('SSH_ORIGINAL_COMMAND'), 'remote_commands_disabled')
    node_id = int(sys.argv[1])
    profile_path = Path('/etc/marzwatch/presence-sources.json')
    st = profile_path.stat()
    require(st.st_uid == 0 and st.st_mode & 0o022 == 0, 'unsafe_source_profile')
    entry = json.loads(profile_path.read_text())[str(node_id)]
    # A simultaneous fleet reboot must not create an unbounded burst of panel
    # imports/database sessions. Busy callers retain unknown data and back off.
    with source_slot():
        source = json.loads(run(['docker', 'exec', '-i', entry['panel_container'], '/code/.venv/bin/python', '-c', PANEL_READ],
                                timeout=10, input=json.dumps({'node_id': node_id})))
    result = minimal_source(source, entry['probe_inbound'])
    # Authorized node collectors need the same private account namespace for
    # cross-node deduplication. It travels only over this restricted SSH channel.
    namespace = json.loads(Path('/etc/marzwatch/presence.json').read_text())['namespace']
    require(isinstance(namespace, str) and 16 <= len(namespace) <= 128, 'invalid_namespace')
    result['namespace'] = namespace
    print(json.dumps(result))


if __name__ == '__main__':
    try: main()
    except Exception:
        # This stdout is consumed over SSH. Never expose diagnostic bodies.
        sys.exit(1)
