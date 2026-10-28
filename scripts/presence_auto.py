"""Bounded, restart-once enrollment of newly matched PasarGuard/monitor nodes."""
import ipaddress
import json
from pathlib import Path
import re
import time
import recover_pasarguard_presence as r
from presence_panel import online_config
from presence_source import minimal_source

STATE = Path('/var/lib/marzwatch-updater/presence-auto.json')
CACHE = Path('/var/lib/marzwatch-updater/presence-panel-cache.json')
BACKUPS = Path('/var/lib/marzwatch-updater/presence-auto-backups')
SCOPE = Path('/etc/marzwatch/presence-auto-scope.json')
REPORTS = Path('/var/lib/marzwatch/presence-enrollment')
MONITORS = Path('/var/lib/marzwatch/state.json')
ID = re.compile(r'[A-Za-z0-9_-]{1,80}')


def ipv4(value):
    try:
        address = ipaddress.ip_address(value)
        if getattr(address, 'ipv4_mapped', None):
            address = address.ipv4_mapped
        return str(address) if address.version == 4 else ''
    except (ValueError, TypeError):
        return ''


def valid_inventory(inv):
    return (isinstance(inv, dict)
        and re.fullmatch(r'[A-Za-z0-9_.-]{1,80}', str(inv.get('node_container', ''))) is not None
        and re.fullmatch('sha256:[a-f0-9]{64}', str(inv.get('node_image_id', ''))) is not None
        and re.fullmatch('[a-f0-9]{64}', str(inv.get('xray_sha256', ''))) is not None
        and isinstance(inv.get('pid'), int) and inv['pid'] > 1
        and re.fullmatch('[0-9]{1,30}', str(inv.get('start_ticks', ''))) is not None)


def matched_nodes(monitors, reports, snapshot, now):
    """Require a unique match on observed peer AND current monitor public IP.

    Names never establish identity. NAT, duplicate panel addresses and competing
    monitor identities remain unconfigured rather than modifying the wrong node.
    """
    found = {}
    for mid, node in monitors.items():
        report = reports.get(mid, {})
        peer = ipv4(report.get('peer_ip'))
        if (mid == 'central' or not ID.fullmatch(mid) or not peer
            or not 0 <= now - report.get('received', 0) <= 600
            or peer != ipv4(node.get('location', {}).get('public_ip'))
            or not valid_inventory(report.get('inventory'))):
            continue
        addresses = {peer}
        # Optional bounded list from the authenticated root agent. A Floating IP
        # need not be the address selected for outgoing monitoring traffic.
        aliases = report['inventory'].get('addresses', [])
        if isinstance(aliases, list) and len(aliases) <= 32:
            addresses.update(ipv4(v) for v in aliases if isinstance(v, str))
        addresses.discard('')
        matches = [n for n in snapshot['nodes'] if ipv4(n['address']) in addresses]
        same_ip = [m for k, m in monitors.items()
                   if k != 'central' and ipv4(m.get('location', {}).get('public_ip')) == peer]
        if len(matches) == 1 and len(same_ip) == 1:
            found[mid] = matches[0]
    # A moved/shared Floating IP must not bind two physical monitors to one
    # panel node. Keep both unresolved until the address ownership is unique.
    return {mid: n for mid, n in found.items()
            if sum(other['id'] == n['id'] for other in found.values()) == 1}


def source_for(node, core, candidates):
    return {'name': node['name'], 'address': node['address'], 'core_id': core['id'],
            'config': core['config'], 'candidates': candidates}


def choose_probe(node, core, candidates):
    cfg = online_config(core['config'])
    source = source_for(node, dict(core, config=cfg), candidates)
    tags = sorted((i.get('tag', '') for i in cfg.get('inbounds', []) if isinstance(i.get('tag'), str)),
                  key=lambda tag: (tag != 'Mostaghim', tag))
    for tag in tags:
        if not tag or len(tag) > 200 or tag in core.get('exclude_inbound_tags', []):
            continue
        profile = {'node_name': node['name'], 'node_address': node['address'],
                   'core_config_id': core['id'], 'probe_inbound': tag}
        try:
            r.validate_policy(minimal_source(source, tag), profile)
            return tag
        except (r.Unverified, KeyError, TypeError):
            pass
    raise r.Unverified('no_supported_runtime_probe')


def profile_for(node, core, inv, probe, options):
    profile = {k: inv[k] for k in ['node_container', 'node_image_id', 'xray_sha256']}
    profile.update(enabled=True, automatic=True, node_id=node['id'], node_name=node['name'],
        node_address=node['address'], core_config_id=core['id'], probe_inbound=probe,
        https_host=options['probe_host'])
    return profile


def refresh_existing_manual(fleet, mid, node, core, report, candidates):
    """Refresh an existing identity after an address/runtime change; never start it."""
    previous = fleet['nodes'].get(mid)
    if (not previous or not previous.get('enabled') or previous.get('node_id') != node['id']
            or node['status'] != 'connected'):
        return
    try:
        if online_config(core['config']) != core['config']:
            return  # An operator disabled tracking; never override that policy.
        probe = choose_probe(node, core, candidates)
        updated = profile_for(node, core, report['inventory'], probe,
                              {'probe_host': previous['https_host']})
        updated.pop('automatic', None)
        fleet['nodes'][mid] = dict(previous, **updated)
    except (r.Unverified, ValueError, KeyError, TypeError):
        return


def reconcile(fleet, snapshot, monitors, reports, state, manual_ids, control, save, now):
    """At most one new activation, with journal-before-side-effect semantics."""
    options = fleet['auto_enroll']
    matches = matched_nodes(monitors, reports, snapshot, now)
    entries = state.setdefault('nodes', {})
    # Expired monitor identities never authorize an action or inflate live scope.
    for mid in list(entries):
        if mid not in monitors:
            entries.pop(mid)
            if fleet['nodes'].get(mid, {}).get('automatic'):
                fleet['nodes'].pop(mid)
    action_used = False
    activation_pending = any(e.get('restart_attempted') and e.get('phase') in
        ['activating', 'activation_unknown', 'verifying'] for e in entries.values())
    for mid, node in sorted(matches.items()):
        entry = entries.get(mid)
        report = reports[mid]
        core = snapshot['cores'].get(str(node['core_id']))
        if not core or core['type'] != 'xray':
            continue
        if mid in manual_ids and mid not in entries:
            refresh_existing_manual(fleet, mid, node, core, report, snapshot['candidates'])
            continue
        if entry and entry.get('phase') == 'ready':
            profile = fleet['nodes'].get(mid, {})
            same = all(profile.get(k) == report['inventory'].get(k) for k in
                       ['node_container', 'node_image_id', 'xray_sha256'])
            same = same and all(profile.get(k) == value for k, value in
                [('node_name', node['name']), ('node_address', node['address']),
                 ('node_id', node['id']), ('core_config_id', node['core_id'])])
            if same:
                continue
            # Follow a legitimate runtime/IP change with a new proof, never a
            # second automatic restart or an override of a user-disabled policy.
            try:
                already_enabled = online_config(core['config']) == core['config']
            except (ValueError, TypeError, KeyError):
                already_enabled = False
            if node['id'] != entry['expected']['id'] or not already_enabled:
                entry['phase'] = 'assignment_changed'; fleet['nodes'].pop(mid, None); continue
            entry.update(phase='rebind', retry_after=0, restart_attempted=False)
        if entry and entry.get('phase') in ['verifying', 'verification_pending', 'activating', 'activation_unknown']:
            if node['core_id'] == entry['target_id']:
                if entry['phase'] in ['activating', 'activation_unknown']:
                    entry['phase'] = 'verifying'
                proof = report.get('proof', {})
                if (report.get('status') == 'ready' and report['received'] >= entry['started']
                    and ((proof.get('existing_account_flow') is True and proof.get('tls_verified') is True)
                         or (proof.get('runtime_tracking_verified') is True
                             and proof.get('verification_method') == 'native_active_accounts'))
                    and node['status'] == 'connected'):
                    entry['phase'] = 'ready'
                elif (node['status'] == 'error' and now - entry['started'] > 180
                      and entry.get('restart_attempted') and not action_used):
                    entry['phase'] = 'rollback_started'; save(); action_used = True
                    try:
                        control('rollback', entry)
                        entry['phase'] = 'rolled_back'
                    except Exception:
                        entry['phase'] = 'rollback_needs_review'
                    fleet['nodes'].pop(mid, None)
                elif now - entry['started'] > 600:
                    entry['phase'] = 'verification_pending'
                continue
            # A successful PUT is never repeated after a timeout/crash. An
            # operator's subsequent assignment also wins over our pending plan.
            if now - entry['started'] > 120:
                entry['phase'] = 'assignment_changed'; fleet['nodes'].pop(mid, None)
            continue
        if entry and entry.get('phase') in ['ready', 'assignment_changed', 'rolled_back',
                'rollback_started', 'rollback_needs_review']:
            continue
        if node['status'] != 'connected' or action_used or activation_pending:
            continue
        if entry and entry.get('retry_after', 0) > now:
            continue
        entry = entries.setdefault(mid, {'monitor_id': mid})
        try:
            probe = choose_probe(node, core, snapshot['candidates'])
            desired = online_config(core['config'])
            entry.update(expected={k: node[k] for k in ['id', 'name', 'address', 'core_id']},
                         source_hash=r.digest(core['config']), started=now, retry_after=now+300)
            if desired != core['config']:
                if not options.get('allow_initial_restart', False):
                    entry['phase'] = 'awaiting_online_policy'; continue
                # Persist before core creation; prepare is idempotent and does
                # not restart any existing node or edit a shared core.
                entry['phase'] = 'preparing'; save(); action_used = True
                target = control('prepare', entry)['target']
                r.require(target['config'] == desired, 'prepared_core_mismatch')
                entry.update(target_id=target['id'], phase='activating', restart_attempted=True)
                fleet['nodes'][mid] = profile_for(node, target, report['inventory'], probe, options)
                save()  # Must survive a lost API response or process termination.
                try:
                    applied = control('activate', entry)['node']
                    r.require(applied['core_config_id'] == target['id'], 'activation_not_applied')
                    entry['phase'] = 'verifying'
                    snapshot['cores'][str(target['id'])] = target
                    node['core_id'] = target['id']
                    node['status'] = applied['status']
                except Exception:
                    entry['phase'] = 'activation_unknown'
            else:
                fleet['nodes'][mid] = profile_for(node, core, report['inventory'], probe, options)
                entry.update(target_id=core['id'], phase='verifying', restart_attempted=False)
                action_used = True
        except (r.Unverified, ValueError, KeyError, TypeError):
            if not entry.get('restart_attempted'):
                entry.update(phase='unsupported_or_unavailable', retry_after=now+300)
    return sorted(mid for mid in entries if mid in monitors)


def panel_call(fleet, action, entry=None):
    options = fleet['auto_enroll']
    r.require(re.fullmatch(r'[A-Za-z0-9.-]{1,253}', options['panel_host']), 'invalid_panel_host')
    r.require(isinstance(options['panel_port'], int) and 1 <= options['panel_port'] <= 65535, 'invalid_panel_port')
    args = dict(entry or {}, action=action, panel_host=options['panel_host'], panel_port=options['panel_port'])
    source = Path(__file__).with_name('presence_panel.py').read_text()
    return json.loads(r.run(['docker', 'exec', '-i', fleet['panel_container'], '/code/.venv/bin/python',
                            '-c', source], input=json.dumps(args), timeout=25))


def tick(cfg, force=False):
    import grp
    import presence_fleet as f
    fleet = f.root_json(f.FLEET)
    options = fleet.get('auto_enroll', {})
    if not options.get('enabled'):
        return f.refresh(force)
    r.require(re.fullmatch(r'[A-Za-z0-9.-]{1,253}', options['probe_host']), 'invalid_probe_host')
    STATE.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    state = f.root_json(STATE) if STATE.exists() else {'nodes': {}}
    monitors = json.loads(MONITORS.read_text()).get('nodes', {})
    r.require(len(monitors) <= 100, 'too_many_monitor_nodes')
    reports = {}
    for mid in monitors:
        if not ID.fullmatch(mid):
            continue
        p = REPORTS / (mid + '.json')
        try:
            if p.stat().st_size <= 16384:
                reports[mid] = json.loads(p.read_text())
        except (OSError, ValueError):
            continue
    cached = f.root_json(CACHE) if CACHE.exists() else {}
    now = time.time()
    manual = set(cfg.get('presence_node_ids') or [])
    pending = any(mid not in manual and mid not in state.get('nodes', {})
                  and valid_inventory(report.get('inventory')) for mid, report in reports.items())
    waiting = any(e.get('phase') in ['verifying', 'activating', 'activation_unknown'] for e in state.get('nodes', {}).values())
    interval = 60 if pending or waiting else 300
    if force or now - state.get('last_panel_read', 0) >= interval or not cached:
        if not force and now < state.get('panel_retry_after', 0):
            return
        state['panel_retry_after'] = now+60
        r.atomic_json(STATE, state)
        cached = panel_call(fleet, 'snapshot')
        r.atomic_json(CACHE, cached)
        state['last_panel_read'] = now
        state['panel_retry_after'] = 0
        r.atomic_json(STATE, state)
    before_state, before_fleet = r.digest(state), r.digest(fleet)

    def save():
        r.atomic_json(STATE, state)
        r.atomic_json(f.FLEET, fleet)

    def control(action, entry):
        if action == 'prepare':
            BACKUPS.mkdir(mode=0o700, parents=True, exist_ok=True)
            backup = BACKUPS / (entry['monitor_id'] + '.json')
            if not backup.exists():
                r.atomic_json(backup, {'node': entry['expected'],
                    'core': cached['cores'][str(entry['expected']['core_id'])]})
        return panel_call(fleet, action, entry)

    scope = reconcile(fleet, cached, monitors, reports, state, manual, control, save, now)
    if before_state != r.digest(state) or before_fleet != r.digest(fleet):
        save()
    # A failed refresh retains previously matched IDs. Stale data is displayed
    # explicitly instead of silently removing a source from the total.
    old_scope = f.root_json(SCOPE) if SCOPE.exists() else {}
    if old_scope.get('ids') != scope:
        r.atomic_json(SCOPE, {'ids': scope}, mode=0o640, gid=grp.getgrnam('marzwatch').gr_gid)
    if (not force and f.PLANS.exists() and now-f.PLANS.stat().st_mtime < 300
        and before_fleet == r.digest(fleet)):
        return
    sources = {}
    for node in cached['nodes']:
        core = cached['cores'].get(str(node['core_id']))
        if core:
            sources[str(node['id'])] = source_for(node, core, cached['candidates'])
    f.publish_plans(fleet, sources)
