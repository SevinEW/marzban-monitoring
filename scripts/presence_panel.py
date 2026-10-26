"""Fixed PasarGuard adapter, executed inside the existing panel container.

Snapshot uses one bounded read-only transaction. Mutations use the supported
HTTPS API with an ephemeral in-memory credential; no direct SQL writes.
"""
import asyncio
import copy
import hashlib
import http.client
import json
import os
import signal
import socket
import ssl
import sys
from datetime import datetime, timedelta, timezone


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()


def online_config(config):
    out = copy.deepcopy(config)
    levels = out.setdefault('policy', {}).setdefault('levels', {})
    if set(levels) - {'0'}:
        raise ValueError('unsupported_user_level')
    for inbound in out.get('inbounds', []):
        if any(c.get('level', 0) != 0 for c in inbound.get('settings', {}).get('clients', [])):
            raise ValueError('unsupported_user_level')
    levels.setdefault('0', {})['statsUserOnline'] = True
    return out


def equivalent_core(core, desired):
    return (all(core.get(k) == desired[k] for k in ['type', 'config'])
            and all(set(core.get(k) or []) == set(desired[k] or []) for k in
                    ['exclude_inbound_tags', 'fallbacks_inbound_tags']))


async def execute(args):
    from sqlalchemy import select, text
    from app.db import GetDB
    from app.db.base import engine
    from app.db.models import Admin, AdminRole, CoreConfig, Node, User
    try:
        if args['action'] == 'snapshot':
            async with GetDB() as db:
                await db.execute(text('SET TRANSACTION READ ONLY'))
                await db.execute(text("SET LOCAL statement_timeout='2000ms'"))
                rows = (await db.execute(select(Node.id, Node.name, Node.address, Node.status,
                    Node.core_config_id, Node.xray_version).order_by(Node.id).limit(101))).all()
                if len(rows) > 100:
                    raise ValueError('too_many_panel_nodes')
                cores = (await db.execute(select(CoreConfig).where(CoreConfig.id.in_(
                    {n.core_config_id for n in rows if n.core_config_id is not None})))).scalars().all()
                users = (await db.execute(select(User.id).where(User.status == 'active')
                    .order_by(User.id).limit(5))).scalars().all()
                def enum(v):
                    return getattr(v, 'value', v)
                return {'nodes': [{'id': n.id, 'name': n.name, 'address': n.address,
                    'status': enum(n.status), 'core_id': n.core_config_id,
                    'xray_version': n.xray_version} for n in rows],
                    'cores': {str(c.id): {'id': c.id, 'name': c.name, 'type': enum(c.type),
                        'config': c.config, 'exclude_inbound_tags': sorted(c.exclude_inbound_tags or []),
                        'fallbacks_inbound_tags': sorted(c.fallbacks_inbound_tags or [])} for c in cores},
                    'candidates': list(users)}

        if args['action'] not in ['prepare', 'activate', 'rollback']:
            raise ValueError('unsupported_action')
        import jwt
        from app.utils.jwt import get_secret_key
        async with GetDB() as db:
            await db.execute(text('SET TRANSACTION READ ONLY'))
            await db.execute(text("SET LOCAL statement_timeout='2000ms'"))
            admins = (await db.execute(select(Admin.id, Admin.username, AdminRole.permissions)
                .join(AdminRole, Admin.role_id == AdminRole.id).where(Admin.status == 'active')
                .order_by(Admin.id).limit(20))).all()
        admin = next(a for a in admins if a.permissions.get('cores', {}).get('create')
                     and a.permissions.get('nodes', {}).get('update'))
        secret = await asyncio.wait_for(get_secret_key(), 5)
        now = datetime.now(timezone.utc)
        token = jwt.encode({'sub': admin.username, 'aid': admin.id, 'access': 'admin',
            'iat': now, 'exp': now + timedelta(minutes=2)}, secret, algorithm='HS256')

        class LocalTLS(http.client.HTTPSConnection):
            def connect(self):
                self.sock = self._context.wrap_socket(socket.create_connection(
                    ('127.0.0.1', args['panel_port']), timeout=5), server_hostname=self.host)

        def api(path, method='GET', body=None):
            connection = LocalTLS(args['panel_host'], args['panel_port'], timeout=8,
                                  context=ssl.create_default_context())
            try:
                connection.request(method, path, body=json.dumps(body) if body is not None else None,
                    headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
                response = connection.getresponse()
                raw = response.read(4 * 1024 * 1024 + 1)
                if not 200 <= response.status < 300 or len(raw) > 4 * 1024 * 1024:
                    raise ValueError('panel_api_failed')
                return json.loads(raw) if raw else None
            finally:
                connection.close()

        expected = args['expected']
        node_id = int(expected['id'])
        node = api('/api/node/' + str(node_id))
        if any(node[k] != expected[k] for k in ['id', 'name', 'address']):
            raise ValueError('node_identity_changed')
        source = api('/api/core/' + str(int(expected['core_id'])))
        if source['type'] != 'xray' or digest(source['config']) != args['source_hash']:
            raise ValueError('source_core_changed')
        desired = online_config(source['config'])
        payload = {k: source[k] for k in ['type', 'exclude_inbound_tags', 'fallbacks_inbound_tags']}
        payload['config'] = desired
        payload['name'] = 'MarzWatch auto online ' + digest(payload)[:16]
        if args['action'] == 'prepare':
            if node['core_config_id'] != expected['core_id'] or node['status'] != 'connected':
                raise ValueError('node_assignment_changed')
            cores = api('/api/cores?limit=100')['cores']
            found = [c for c in cores if equivalent_core(c, payload)]
            target = min(found, key=lambda c: c['id']) if found else api('/api/core', 'POST', payload)
            return {'target': target}
        target = api('/api/core/' + str(int(args['target_id'])))
        if not equivalent_core(target, payload):
            raise ValueError('target_core_changed')
        if args['action'] == 'activate':
            if node['core_config_id'] == target['id']:
                return {'node': {k: node[k] for k in ['id', 'status', 'core_config_id']}}
            if node['core_config_id'] != expected['core_id'] or node['status'] != 'connected':
                raise ValueError('node_assignment_changed')
            node = api('/api/node/' + str(node_id), 'PUT', {'core_config_id': target['id']})
        else:
            if node['core_config_id'] != target['id'] or node['status'] != 'error':
                raise ValueError('rollback_not_required')
            node = api('/api/node/' + str(node_id), 'PUT', {'core_config_id': source['id']})
        return {'node': {k: node[k] for k in ['id', 'status', 'core_config_id']}}
    finally:
        await engine.dispose()


if __name__ == '__main__':
    try:
        # docker exec runs outside the helper's cgroup. Bound its own lifetime
        # (including blocking HTTPS calls) and lower its scheduling priority.
        os.nice(15)
        signal.alarm(20)
        arguments = json.load(sys.stdin)
        print(json.dumps(asyncio.run(asyncio.wait_for(execute(arguments), 40))))
    except Exception:
        # API responses and exceptions may contain credentials: never log them.
        print('{"error":"panel_operation_failed"}')
        raise SystemExit(1)
