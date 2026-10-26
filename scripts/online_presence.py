"""Read-only Xray presence prototype; not a device counter or installed daemon.

One bulk RPC per core. User identities stay in memory for cross-core deduplication.
Only aggregate counts leave public_snapshot(). Runtime tracking must be verified
by the caller; a reachable API or an empty response does not prove coverage.
Requires grpclib and protobuf only when using XrayPresenceClient.
"""
from dataclasses import dataclass, field
import asyncio
import ipaddress
import math
import time


@dataclass(frozen=True)
class Observation:
    users: frozenset[str] = field(default_factory=frozenset, repr=False)
    received: float = 0.0  # monotonic receipt time, not a client-supplied timestamp
    status: str = "unavailable"
    latency_ms: float | None = None


def normalize_users(values):
    """Xray v26.3.27 returns counter names, not plain account identifiers."""
    if not isinstance(values, (list, tuple)) or len(values) > 100_000:
        raise ValueError("invalid presence response")
    users = set()
    for value in values:
        if not isinstance(value, str) or len(value) > 1024:
            raise ValueError("invalid presence identity")
        prefix, suffix = "user>>>", ">>>online"
        if not value.startswith(prefix) or not value.endswith(suffix):
            raise ValueError("unsupported presence identity format")
        identity = value[len(prefix):-len(suffix)]
        if not identity or ">>>" in identity or any(ord(c) < 32 for c in identity):
            raise ValueError("invalid presence identity")
        users.add(identity)
    return frozenset(users)


def public_snapshot(observations, expected_nodes, now=None, max_age=3.0):
    """Never present a partial sum as the fleet total or missing data as zero.

    All cores must use the same account namespace (one PasarGuard deployment).
    Inputs are current inventory, not a historical list of retired nodes.
    """
    if not math.isfinite(max_age) or max_age <= 0:
        raise ValueError("max_age must be positive")
    now = time.monotonic() if now is None else now
    if not math.isfinite(now):
        raise ValueError("invalid receipt clock")
    nodes, unique = {}, set()
    covered = 0
    for node_id in sorted(set(expected_nodes)):
        obs = observations.get(node_id, Observation())
        age = now - obs.received
        status = obs.status
        if status == "ok" and (not math.isfinite(age) or age < 0 or age > max_age):
            status = "stale"
        valid = status == "ok"
        if valid:
            covered += 1
            unique.update(obs.users)
        nodes[node_id] = {
            "status": status,
            "users": len(obs.users) if valid else None,
            "latency_ms": obs.latency_ms,
        }
    complete = bool(nodes) and covered == len(nodes)
    return {
        "schema": 1,
        "metric": "accounts_with_open_xray_flows",
        "users": len(unique) if complete else None,
        "observed_users": len(unique) if covered else None,
        "coverage": {"ready": covered, "expected": len(nodes), "complete": complete},
        "devices": None,  # The proxy protocol supplies no independent device ID.
        "nodes": nodes,
    }


def _messages():
    from google.protobuf import descriptor_pb2, descriptor_pool, message_factory
    schema = descriptor_pb2.FileDescriptorProto(
        name="marzwatch_presence.proto", package="marzwatch.presence", syntax="proto3"
    )
    schema.message_type.add(name="Request")
    response = schema.message_type.add(name="Response")
    response.field.add(name="users", number=1, label=3, type=9)
    pool = descriptor_pool.DescriptorPool()
    pool.Add(schema)
    return tuple(message_factory.GetMessageClass(pool.FindMessageTypeByName(
        "marzwatch.presence." + name)) for name in ("Request", "Response"))


class XrayPresenceClient:
    """Persistent loopback channel: never opens a public stats listener."""
    def __init__(self, port, host="127.0.0.1"):
        if not ipaddress.ip_address(host).is_loopback or not 0 < port < 65536:
            raise ValueError("presence API must be a loopback endpoint")
        from grpclib.client import Channel, UnaryUnaryMethod
        from grpclib.encoding.proto import ProtoCodec

        request, response = _messages()

        class BoundedCodec(ProtoCodec):
            def decode(self, data, message_type):
                if len(data) > 4 * 1024 * 1024:
                    raise ValueError("oversized presence response")
                return super().decode(data, message_type)

        self.channel = Channel(host=host, port=port, codec=BoundedCodec())
        self.request = request
        self.query = UnaryUnaryMethod(self.channel,
            "/xray.app.stats.command.StatsService/GetAllOnlineUsers", request, response)
        self.lock = asyncio.Lock()

    async def sample(self, tracking_verified=False, timeout=1.0):
        # A trusted runtime/config lifecycle adapter must verify all user levels.
        # Do not allow an API success to turn a disabled tracker into a false zero.
        if tracking_verified is not True:
            return Observation(status="tracking_unverified")
        if not math.isfinite(timeout) or timeout <= 0:
            raise ValueError("timeout must be positive")
        if self.lock.locked():
            return Observation(status="busy")
        async with self.lock:
            start = time.monotonic()
            try:
                response = await asyncio.wait_for(self.query(self.request(), timeout=timeout), timeout)
                users = normalize_users(list(response.users))
                end = time.monotonic()
                return Observation(users, end, "ok", round((end-start)*1000, 2))
            except asyncio.TimeoutError:
                return Observation(status="timeout")
            except Exception:
                # Exceptions may contain private identities: never log their text.
                return Observation(status="unavailable")

    def close(self):
        self.channel.close()
