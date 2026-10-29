# 💠 MarzWatch

Lightweight, fail-safe monitoring for Marzban/Xray node fleets with Telegram reporting.

## Design rules (standard monitor)
- Does **not** modify Marzban, Xray, Docker, firewall, routes, DNS or certificates.
- Agent reads lightweight Linux counters from `/proc` and `statfs`.
- One static Go binary; no Python/Redis/PostgreSQL/Docker dependency.
- Five-second local sampling and healthy-path metric delivery on shared clock boundaries.
- Daily traffic and reports use Asia/Tehran; the current-day counter rolls over at Iran midnight (00:00).
- TLS certificate fingerprint is pinned inside the Join Key.
- HMAC-signed node metrics.
- Central and agent are systemd resource-limited.
- State snapshots are bounded and flushed at a low frequency to reduce disk writes.

## Optional PasarGuard rolling traffic

An explicitly enabled [PasarGuard adapter](docs/pasarguard-traffic.md) replaces
forum traffic fields with independent Xray inbound accounting over the preceding
24 hours. It uses a small collector in the existing panel container, needs inbound
stats enabled on the relevant Xray cores, and never resets the panel's counters.
It excludes host/API traffic, preserves the HUD, and labels incomplete coverage
with `*`. The standard installer does not configure this adapter or change proxy
cores. See the guide for deployment, resource bounds, accuracy and rollback.

## Install UX
```bash
bash <(curl -fsSL https://raw.githubusercontent.com/SevinEW/marzban-monitoring/main/scripts/install.sh)
```
Then choose:
1. Central Server
2. Node Server

### Central
Prompts for Telegram bot token, admin chat ID, port and name (daily accounting uses Asia/Tehran). It creates a Join Key after startup.

### Node
Prompts for central IP, central port and Join Key. It registers automatically.

## Commands
```bash
marzwatchctl doctor
marzwatchctl join-key   # central only
marzwatchctl uninstall
journalctl -u marzwatch -f
```

## Default resource guard
The systemd unit caps MarzWatch at 20% of one CPU and 128 MB RAM. Monitoring failure cannot restart or alter Marzban/Xray.

## Current scope (v0.1.0)
- CPU / load
- RAM / Swap
- Root filesystem usage
- RX / TX live bandwidth and byte counters
- Uptime
- Public IPv4 and country/city discovery
- Central self-monitoring
- Node registration and TLS fingerprint pinning
- HMAC authenticated metrics
- Telegram Forum live cards with natural server ordering and in-place updates
- CPU/RAM/Disk sustained alerts + recovery
- Offline / recovered node alerts
- Daily traffic and average summary

## Safety
The installer never executes `apt upgrade`, `docker prune`, `iptables`, `ufw`, `sysctl`, or service restarts for Xray/Marzban.

## Live update timing

Install/update Central first, then all nodes, to use the same five-second cadence.
The installer downloads the verified `latest` release; an existing installation
can use menu option 6 to update without deleting its configuration or state.

- After one startup sample, collectors target seconds 00, 05, 10, and so on.
  Nodes estimate Central clock offset from short pinned-TLS exchanges; this does
  not change the operating system clock. Network asymmetry and scheduling mean
  synchronization is approximate. Older Centrals without the time header use
  each node's system clock until upgraded.
- One request per node can be in flight. A one-slot mailbox replaces old pending
  samples with the newest sample. Slow requests and outages do not accumulate a
  replay queue. Failures back off from 5 seconds to at most 60 seconds; collection
  continues, and successful requests resume normal delivery.
- Telegram topics share a paced write queue. Each card is rendered from the latest
  store data **after** waiting for its turn, preserving the existing HUD. Cards
  traverse the naturally sorted node list; Telegram's own activity-based topic
  display order is outside the bot's control.
- The conservative default allows one group write every 3.2 seconds, measured
  from request start. Explicit Telegram `retry_after` always takes precedence.
  With 17 node cards plus Overview, a complete steady-state round takes about
  58 seconds before extra delays, alerts, topic maintenance or rate-limit waits.
  This is not a promise of simultaneous five-second Telegram edits. Telegram's
  [published limits](https://core.telegram.org/bots/faq#my-bot-is-hitting-limits-how-do-i-avoid-this)
  describe message sending; edit capacity is not guaranteed by that guidance.
- Collection was already every 5 seconds in the preceding release. The main
  additional load is roughly twice as many metric requests versus its nominal
  10-second delivery interval (for example, 20 nodes average 4 requests/second).
  Pending memory is bounded and the storage flush interval is unchanged. Actual
  CPU, memory and wire traffic depend on the host and network and need runtime
  measurement; the five-second setting does not imply twice the total CPU use.

Release builds apply the source transforms listed in `.github/workflows/release.yml`
before testing and compiling. Run that complete sequence when building from source.

## Location repair and quiet topics

Country grouping now precedes natural name sorting in Overview. Topic names use
stable country labels (for example `TR` plus the server name), so registering a
node does not renumber and rename all other topics. Telegram controls its own
activity-based topic ordering; the Bot API cannot force a permanent sidebar order.

IP detection prefers a single public non-tunnel interface IPv4, then tries two
external IP endpoints. Geolocation compares ipwho.is and ipapi.co for that exact
IP. Disagreeing countries are left unknown; disagreeing cities are omitted.
A single available provider remains a best-effort estimate, not physical-location
proof. NAT/VPN egress and inaccurate IP registries may still require an override.
Existing agents advertise their newly detected IP after restarting. Authenticated
connections fill missing IPs on Central; Central periodically retries incomplete
metadata with backoff and refreshes successful lookups daily. Identity and traffic
history are retained.

On Central, explicitly correct a known country without guessing from a node name:

```bash
marzwatchctl location Turkey2 TR
systemctl restart marzwatch
```

An optional city follows the country code. Use `auto` instead of a country to
remove an override. Node names must uniquely match one saved node; duplicate names
require the node ID. Preferences are stored by node ID and survive normal updates.

A dedicated update consumer deletes only rename service messages authored by this
bot in its managed topics, when Telegram delivers those events. It persists its
cursor, respects the shared write pacing, and stops if a webhook or another poller
conflicts. It does not scan/delete arbitrary history or monitor cards. Events older
than Telegram's update queue cannot be recovered this way; historical notifications
may need manual deletion, and Telegram's message-deletion age limits still apply.

## Iran daily traffic

Traffic TODAY is the Iran calendar day starting at 00:00, not a rolling 24-hour window. Central uses the same embedded Asia/Tehran timezone for accounting, live cards and daily reports, independently of the host timezone or an older configured timezone. History remains saved; midnight selects the new day instead of erasing yesterday. Cards show the new day on their next Telegram update.

Counter intervals crossing midnight are divided in proportion to elapsed time, preserving the total byte count. With five-second samples, the exact per-packet distribution inside that boundary interval is not known. Existing historical daily totals are retained and cannot be rebucketed from aggregates if a previous installation used a different timezone; the first full Iran day after upgrading is the clean comparison period. Restarting does not reset saved daily totals.


## Removing a monitoring node

The web card X confirms the exact node and IP. A node with a recent authenticated
heartbeat receives a fixed MarzWatch-only uninstall action over its existing pinned
TLS connection; no SSH is required. The card remains pending until the node confirms
cleanup. The embedded root helper removes only MarzWatch services, program,
configuration, data and its service account. It does not change PasarGuard, Xray,
Docker, firewall rules or host swap. Central cannot be removed from a card.

An offline node is removed from the central list only, with no deferred uninstall.
Revoked identities cannot reappear from late metric packets; an intentional fresh
installation can register a new identity. Online removal requires an updated agent
and an authenticated, same-origin web session. Expired or unacknowledged actions
remain visibly incomplete rather than claiming success.

Reinstalled monitoring agents can enroll presence automatically once the previous
same-IP identity has both Central contact and enrollment reports older than ten
minutes. Fresh, conflicting identities remain blocked; matching never uses names.
