# Automatic presence enrollment

The existing root updater installs fixed Python helpers embedded in the verified
MarzWatch release. It enables a bounded oneshot timer; it does not execute code
received from Central. An older updater bootstraps the helpers on its next timer
run after downloading the new updater. New installs get the same behavior.

If GitHub downloads fail, an updated agent can fetch the verified release bundle
from Central over its existing pinned TLS connection and node identity. Central
caches both architectures' verified binaries, updater and checksum manifest after a
successful update, retaining at most three bundles. Only those fixed asset names
are served. The agent verifies the entire bundle before publishing any input to
the installer; checksum failures leave the installation unchanged. This fallback
requires a one-time upgrade from legacy agents that only know GitHub downloads.

Agents use their existing per-node identity and pinned Central TLS certificate.
The `presence-enroll-v1` authentication domain is separate from metrics/presence.
Manual mode accepts only active IDs explicitly present in `presence_node_ids`.
With `presence_auto_enroll: true`, active authenticated agents may also submit
discovery reports. Central records the observed connection IP itself, returns
only that node's approved plan, and persists one bounded latest report. Discovery
alone never adds a host to the online count. Reports are private local files.

## Automatic new-node enrollment

The root-owned fleet configuration can opt into the integrated PasarGuard adapter:

```json
"auto_enroll": {
  "enabled": true,
  "allow_initial_restart": true,
  "panel_host": "PANEL_CERTIFICATE_HOSTNAME",
  "panel_port": 9991,
  "probe_host": "CENTRAL_HTTPS_CERTIFICATE_HOSTNAME"
}
```

The panel API is contacted on loopback with normal certificate validation. The
adapter uses a short-lived in-memory credential and supported API operations;
it never writes SQL or creates, removes, or modifies user accounts.

Every candidate must match exactly one connected PasarGuard Xray node by its
observed source IPv4 and current monitor public IPv4. Duplicate addresses,
competing monitor identities, stale discovery, WireGuard nodes, unsupported
probe configurations, and hosts without a PasarGuard node are not activated.
The agent independently verifies Docker parent/process ownership, its local IP,
the pinned executable and container, and a real existing-account connection.
Initial runtime hashes come from authenticated local discovery and are pinned
to that matched node; they are not a global approval of arbitrary executables.

If tracking is already enabled, enrollment makes no panel changes. Otherwise,
with explicit `allow_initial_restart`, it creates or reuses a separate config
that differs only by the level-zero online flag and assigns it to that one new
node. Shared source configs remain unchanged. At most one initial restart is
in flight; it is journaled before the API call and is not repeated after a lost
response. A failed connection gets one guarded rollback, while a failed runtime
proof stays unknown and retries without restarting a healthy core. Manual nodes
and later user changes that disable tracking are never overridden.

Normal discovery refresh uses one bounded read-only transaction every five
minutes (node metadata, distinct core configs, five candidate account IDs).
Pending discovery/activation may refresh once per minute, regardless of fleet
size, up to 100 nodes. Idle 30-second ticks read small local files and make no
database calls. Prepared plans renew every five minutes. The live counters still
use one persistent native bulk RPC per second; no per-account database polling.

The dynamic count scope is written separately, so new cards and the total update
without a Central restart. Known sources remain visible as unknown/stale if
verification or collection fails. Existing ordinary host cards keep no count.

New installs start the embedded enrollment timer before attempting the optional
GitHub updater bootstrap. No separate agent command or SSH is needed once the
normal PasarGuard node and current MarzWatch agent installations are complete.
IPv4 host-network PasarGuard/Xray with a level-zero VLESS/WS inbound is currently
supported for the real-flow proof; other configurations fail closed.

Operational state and original-config backups are root-only under
`/var/lib/marzwatch-updater/presence-auto*`. Disabling `auto_enroll.enabled`
stops further automatic mutations and retains existing approved plans.

## Manual enrollment

Enrollment first reports the existing PasarGuard container image and Xray digest.
An operator approves the exact runtime and node/core assignment on Central in
root-owned `/etc/marzwatch/presence-fleet-config.json`:

```json
{
  "panel_container": "EXISTING_PANEL_CONTAINER",
  "nodes": {
    "MONITOR_ID": {
      "enabled": true,
      "node_id": 123,
      "node_name": "NODE_NAME",
      "node_address": "NODE_IPV4",
      "core_config_id": 456,
      "node_container": "EXISTING_NODE_CONTAINER",
      "node_image_id": "sha256:APPROVED_IMAGE_DIGEST",
      "xray_sha256": "APPROVED_EXECUTABLE_DIGEST",
      "probe_inbound": "EXISTING_VLESS_WS_INBOUND",
      "https_host": "CENTRAL_HTTPS_CERTIFICATE_NAME"
    }
  }
}
```

The Central timer uses one bounded read-only transaction every five minutes to
refresh minimal plans. Only plans whose current name, address, core assignment,
and level-zero online policy match are published. Plans expire after 11 minutes.
No client UUIDs, credentials, full configurations or arbitrary commands are sent.
The private shared account namespace travels only over the pinned channel.

In manual mode, online tracking must be enabled separately through the panel,
preferably with a private core clone. Agent-side enrollment never changes policy,
creates users or restarts VPN cores. It proves the running core
using an existing offline account: the account becomes online during a short
TLS-verified local flow to Central and clears after the flow closes. A failed
proof leaves the count unknown, never a fabricated zero.

The agent checks process/boot metadata locally every 30 seconds. A valid binding
avoids Docker, SSH and database work; it sends a small readiness report every five
minutes. Failed proofs back off for five minutes. Generation changes require a
fresh plan and proof. Host workers have 20% CPU, 128 MiB memory and 90-second runtime
limits. Each short-lived adapter inside the existing panel container has lower
scheduling priority, a 20-second hard lifetime and a two-second SQL statement
timeout; its resource use belongs to that container, not the host worker cgroup.
Collection remains the existing persistent Go RPC client, with one bulk
snapshot per second and delta transmission, not per-user polling.

Central operator diagnostics: `/var/lib/marzwatch/presence-enrollment/`.
Agent diagnostics: `/var/lib/marzwatch-updater/presence-enrollment.json` and
`journalctl -u marzwatch-presence-enroll.service`. Never publish raw plan/profile
files because they include the private deduplication namespace.

For an explicit immediate plan refresh on Central:
`python3 /usr/local/lib/marzwatch/presence_fleet.py --refresh`.

Tests cover TLS pin-before-credentials, endpoint authentication and per-node plan
isolation, excluded hosts, expiry, replay write limits, runtime/config mismatch,
and rejection of arbitrary plan commands, paths and destinations.
