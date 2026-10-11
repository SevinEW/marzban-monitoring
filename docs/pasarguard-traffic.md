# Independent PasarGuard rolling traffic

This optional adapter changes forum live cards from host-interface daily totals
to **PasarGuard Xray inbound bytes over the preceding 86,400 seconds**. It does
not reset at midnight. Existing layouts and custom emoji are preserved. CPU,
RAM, disk and health remain host metrics; `PG NETWORK` is proxy traffic.

## Accounting and accuracy

* Read configured inbound tags using `StatType.Inbounds` with `reset=False`.
  Internal API traffic is excluded. No users, packets or logs are collected.
* Never consume/reset the user or outbound counters used by PasarGuard.
* Keep durable per-node deltas in SQLite WAL for 25 hours. A meter restart
  resumes the saved baseline; duplicate/out-of-order samples are ignored.
* Check Xray backend uptime as well as decreasing/changing counters. A detected
  reset or collection gap marks coverage incomplete, rather than inventing bytes.
* The first reading is a baseline, not lifetime usage. `*` means less than a full
  uninterrupted 24-hour history. Historical traffic before installation cannot
  be reconstructed. `N/A` means no fresh usable source or no node mapping.
* The window edge is prorated within a nominal 10-second sample. These are
  proxy application counters, not a packet-exact wire billing system. Traffic
  between the last sample and a core crash may be lost and is marked incomplete.
  A restarted core first establishes a new baseline.
* Overview sums unique panel IDs, never monitor aliases. If a node is stale,
  its retained history is included and the total is marked partial; stale
  rates are excluded. Host-only relay cards do not enter the proxy total.

## Resource budget

Two small stats requests per node every 10 seconds, maximum three concurrent
collectors. Failed nodes back off to 60 seconds. Inventory reads only node/core
rows once per minute, through one short-lived DB connection. The collector has
a singleton lock and lowered CPU priority; it never imports the panel application.
At 17 nodes, 25 hours is at most about 153,000 interval rows. SQLite reuses expired
pages. The JSON handoff contains only current aggregate values, not history.
The service unit manages a process inside the existing container; Docker CLI
cgroup limits would not constrain that process, so no such limit is claimed.

## Deployment

Tested with PasarGuard panel 5.2.1 / NodeBridge 0.8.1 / PostgreSQL. The adapter
requires Python 3.11+, `asyncpg`, the installed NodeBridge API, and
`SQLALCHEMY_DATABASE_URL` in the panel container environment. It supports both
gRPC and REST nodes running Xray; other cores are excluded.

1. Back up the relevant core JSON and MarzWatch config/state/forum/binary.
2. In the applicable Xray core, enable only
   `policy.system.statsInboundUplink` and `statsInboundDownlink`. Save without
   restarting all nodes. Reconnect and validate one canary, then apply one node
   at a time, checking connectivity. This can briefly interrupt proxy sessions.
3. Install `scripts/pg_meter.py` as
   `/var/lib/pasarguard/marzwatch-meter/pg_meter.py` (directory 0700, file 0600)
   on the shared panel data volume. Install the supplied service unit, adapting
   `PG_CONTAINER` if its name differs. Start and inspect the meter before enabling
   MarzWatch's adapter. The SQLite database stays private; the aggregate handoff
   `/var/lib/pasarguard/marzwatch-traffic.json` is readable by MarzWatch.
4. Configure Central with `pasarguard_traffic: true` and `pasarguard_nodes`, a
   JSON object mapping stable MarzWatch IDs to string panel node IDs. Match real
   identities, not just names or shared IPs. IP changes do not change this mapping.
   New/reinstalled identities need an explicit verified mapping.
5. Install the release containing `apply_pg_traffic.py`, restart MarzWatch, and
   verify a node card and Overview. Enable `marzwatch-pg-meter.service` at boot.

The standard installer does not alter PasarGuard or enable this opt-in adapter.
Non-forum legacy daily reports retain their existing semantics. Panel/node
credentials are read only in memory and never included in the handoff or logs.
Keep deployments and mappings out of the public repository.

## Rollback

Disable `pasarguard_traffic` and restart MarzWatch to restore host daily cards.
Stop/disable the meter; retain its private SQLite history if needed. Inbound
stats can remain enabled safely, or restore the saved core policy and reconnect
nodes one at a time. Do not delete existing monitoring state or proxy data.
