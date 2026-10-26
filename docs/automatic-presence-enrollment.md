# Automatic presence enrollment

The existing root updater installs fixed Python helpers embedded in the verified
MarzWatch release. It enables a bounded oneshot timer; it does not execute code
received from Central. An older updater bootstraps the helpers on its next timer
run after downloading the new updater. New installs get the same behavior.

Agents use their existing per-node identity and pinned Central TLS certificate.
The `presence-enroll-v1` authentication domain is separate from metrics/presence.
Central accepts only active IDs explicitly present in `presence_node_ids`, returns
only that node's plan and persists one bounded latest report. Non-VPN hosts are
not enrolled. Reports are private local files, not a public web endpoint.

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

Online tracking must be enabled separately through the panel, preferably with a
private core clone and one-node-at-a-time activation. Enrollment itself never
changes policy, creates users or restarts VPN cores. It proves the running core
using an existing offline account: the account becomes online during a short
TLS-verified local flow to Central and clears after the flow closes. A failed
proof leaves the count unknown, never a fabricated zero.

The agent checks process/boot metadata locally every 30 seconds. A valid binding
avoids Docker, SSH and database work; it sends a small readiness report every five
minutes. Failed proofs back off for five minutes. Generation changes require a
fresh plan and proof. Workers have 20% CPU, 128 MiB memory and 60-second runtime
limits. Collection remains the existing persistent Go RPC client, with one bulk
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
