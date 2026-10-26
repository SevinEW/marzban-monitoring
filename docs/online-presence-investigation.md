# Independent online-presence prototype — 2026-10-26

Status: Go collector, authenticated delta transport, in-memory aggregation and web
indicators implemented. Production activation remains opt-in and requires verified
tracking on each running core. This is an account-presence collector, not a
physical-device counter. Existing production services were not restarted during tests.

## Findings from the installed system

- Installed panel image: `pasarguard/panel:v5.4.1`.
- The installed `app/operation/system.py` calls `get_users_count_metrics()` with
  a two-minute online window. `app/db/crud/user.py` filters on `User.online_at`.
- The installed `app/jobs/record_usages.py` updates `online_at` when the recorded
  traffic delta is positive. This measures recently observed traffic, not
  necessarily an open flow at the moment of display.
- The saved core configuration for the local Master node has no online policy.
  PasarGuard Node v0.5.3's `checkPolicy()` leaves `StatsUserOnline` false unless
  explicitly enabled. This is config/source evidence, not a dump of effective
  running configuration.
- The reachable local Xray API returned an empty online-user response. Given the
  missing tracking configuration, this cannot establish zero online users.
- One aggregate recent-activity SQL query hit its four-second deadline. It was
  not used as evidence of zero users or as proof of incorrect panel totals.

Sources:
- https://github.com/PasarGuard/node/blob/v0.5.3/backend/xray/config.go
- https://github.com/XTLS/Xray-core/blob/v26.3.27/app/stats/stats.go
- https://github.com/XTLS/Xray-core/blob/v26.3.27/app/stats/online_map.go
- https://github.com/XTLS/Xray-core/blob/v26.3.27/app/stats/command/command.proto

## Implemented prototype

`scripts/online_presence.py` reads `GetAllOnlineUsers` with a persistent local
gRPC channel. It makes one bulk request per core instead of one request per user.
It performs no counter reset, database write, core configuration change, packet
capture, or user enumeration request to PasarGuard.

Users are deduplicated across cores in the same PasarGuard account namespace.
Identities remain in process memory; the public snapshot contains only counts,
coverage and source status. Missing, unverified, stale, invalid, or failed data
does not become zero. A partial observation is separate from a full fleet total.
Overlapping polls are rejected rather than queued. Device count remains null.

## Validation

Unit tests cover deduplication, stale data, clock anomalies, unknown tracking,
partial coverage, privacy, true zero, malformed identifiers and overlapping polls.

The integration harness started a separate Xray v26.3.27 process with new
loopback-only listeners, temporary in-memory credentials, one Go scheduler thread
and a soft 32 MiB Go memory target. All traffic terminated at a local echo server;
there was no external traffic and no production core modification.

Six real-core scenarios passed:

| Scenario | Accounts observed |
| --- | ---: |
| Empty core with verified tracking | 0 |
| One account, one open flow | 1 |
| Same account, four flows from the same IP | 1 |
| Two accounts sharing that same IP | 2 |
| First account's flows closed | 1 |
| All flows closed | 0 |

Individual local bulk RPCs took 2.61–4.72 ms in this tiny test. The temporary core
used 29,760 KiB RSS. These are test observations, not production capacity or
collector-memory claims. A first attempt to assert removal after 200 ms failed;
the bounded closure test observed removal around one second after closing the
test sockets. Transport shutdown and network failure detection are not instant.

## Activation requirements

The integrated implementation below replaces the initial prototype. Production
activation still requires verified `statsUserOnline` for every active user level
on each running core. Existing cores need a config reload/restart to enable it;
shared PasarGuard templates must not be modified without considering all affected
nodes. A currently active core must not be silently restarted.

Exact physical-device counting requires a per-device identity participating in
the connection lifecycle. IPs, transport flows and shareable credentials cannot
establish that identity for existing generic clients. The device side of the
requested device/user pair therefore stays unavailable.

## Integrated implementation

The Go collector in `internal/presence` is part of the existing monitor, with no
additional Python process or database service. Its persistent gRPC channel reads
one bulk snapshot per core each second. The sender transmits changes and small
heartbeats; a full snapshot is sent after uncertainty/reconnect and every minute.
Requests use the existing pinned TLS connection and node HMAC with a separate
presence signature domain. Sequence numbers advance on every send attempt, so a
lost acknowledgment cannot leave phantom users. Four presence requests may be
processed concurrently; body size, per-node identities, total identities, source
count and request duration are bounded.

Identifiers are pseudonymized with the shared panel namespace, kept in memory
only, and never included in web frames or monitor state files. Stale or unverified
sources cannot report zero. Global totals require full coverage. The UI displays
the observed account count with its source coverage when only part of the fleet
is ready. Physical devices are not inferred. Touch/click help and card details
explain the meaning; network-stream failure clears visible counts.

A root-owned `/etc/marzwatch/presence.json` binds the source to an existing loopback
API and the actual Xray PID/start ticks. `presence-bind` also verifies ownership of
the API socket. The operator must verify `statsUserOnline` for all active user
levels before attesting. A core restart invalidates the binding; counts remain
unavailable until reverified/rebound. This deliberately cannot silently reinterpret
an empty response from a newly started, unconfigured core as zero.

## Opt-in restart recovery

Central can set `presence_node_ids` to the verified monitor IDs of this panel's
VPN nodes. An explicit empty list disables all presence counters; missing/null
preserves the legacy all-host scope. Nodes outside the list keep their CPU,
bandwidth and traffic cards, but have no online-user badge/detail rows and do not
contribute to presence coverage or totals. An unavailable VPN source remains in
coverage: lack of data must not silently become a complete total. Update this
explicit inventory when enrolling or retiring VPN nodes; no periodic database
query is added by this setting.

A host reboot changes both Xray's PID and PasarGuard Node's randomly selected
loopback stats port. A PID-only refresh is insufficient. New bindings also carry
the Linux boot ID to prevent accidental PID/start-tick reuse across boots.

`scripts/recover_pasarguard_presence.py` and the matching systemd service/timer
provide an explicitly configured adapter for the already-enabled single-level
PasarGuard pilot. They are **not installed or enabled by the fleet updater**.
Install the script at `/usr/local/lib/marzwatch/recover_pasarguard_presence.py`
and the service/timer in `/etc/systemd/system/` only on the opted-in host. Create
a root-owned, mode-0600 `/etc/marzwatch/presence-recovery.json` containing:

```json
{
  "node_name": "the exact opted-in node name",
  "node_address": "the host's own IPv4 address",
  "node_id": 0,
  "core_config_id": 0,
  "node_container": "the verified local node container",
  "panel_container": "the local PasarGuard panel container",
  "node_image_id": "sha256:the reviewed image digest",
  "xray_sha256": "the reviewed running executable digest",
  "probe_inbound": "an existing VLESS WebSocket inbound tag",
  "https_host": "the existing local HTTPS endpoint's certificate name"
}
```

These are deployment values, not defaults to copy literally. The adapter requires
the existing node to run with host networking, the existing panel Python path
`/code/.venv/bin/python`, level-zero online tracking, a plain WebSocket inbound,
and valid TLS on this host's own port 443. It refuses unknown images, changed
assignments, additional user levels or unsupported layouts. Re-attest software
updates explicitly; an empty online RPC alone is never proof of tracking.

Every 30 seconds the normal path reads only the local binding, boot ID, state and
process start ticks. It does not open a database connection, run Docker commands
or make a network request. When the generation changes, recovery:

1. Verifies the exact container image, Xray executable and parent process.
2. Reads this node's configuration and at most five candidate account IDs in a
   read-only database transaction with a two-second statement deadline.
3. Discovers the stats API among at most four loopback listeners owned by that
   Xray process; configured client inbounds are excluded.
4. Proves the running online policy with one existing account and a short local
   VPN flow to the same host's verified HTTPS endpoint. A temporary client uses
   one Go thread and a 32 MiB soft memory target and is always cleaned up.
5. Rechecks process/socket ownership and atomically replaces only the monitor's
   binding. It preserves the account namespace. Monitoring reloads automatically.

No panel/core restart, SQL write, new account, policy edit or counter reset is
performed. Failed recovery stays unavailable and retries no more than once per
five minutes. The one-shot service caps its process group at 128 MiB and 20% of
one CPU; it has no always-running Python daemon. The read-only panel query runs
inside the existing panel container only during recovery. The lifecycle of the
other fleet nodes is not changed.

After installation, run the helper once with `--verify`, then enable
`marzwatch-presence-recovery.timer`. Validate the live count, service result and
unchanged Xray PID. Roll back the adapter with `systemctl disable --now
marzwatch-presence-recovery.timer`; the collector still fails closed after future
core changes and supports manual attestation. Keep a private binding backup before
deployment. Do not paste bindings, account responses or configuration contents in
logs. The helper logs only controlled result codes and the non-secret process ID.

Use one shared random namespace for all nodes of the same PasarGuard instance;
never reuse it across unrelated panels. Existing installations are inert until
bound. The binding command does not enable the Xray policy or restart the core.

Validation on Master (isolated listeners, no external test traffic): all six
real-core scenarios also passed with the Go binary. Cold CLI collection took
1.99–6.134 ms per sample in the two-account test. The aggregation benchmark with
24 nodes / 8,000 unique accounts took about 2.05 ms per snapshot, allocating about
877 KB per snapshot on that host. This is aggregation cost, not a claim about full
fleet CPU/RSS. Existing Central tests, new authentication/coverage/sequence tests,
and JavaScript numerical/stale-data regression checks passed. Browser visual QA
has not completed because the browser runtime reports an environment error.
