# 💠 MarzWatch

Lightweight, fail-safe monitoring for Marzban/Xray node fleets with Telegram reporting.

## Design rules
- Does **not** modify Marzban, Xray, Docker, firewall, routes, DNS or certificates.
- Agent reads lightweight Linux counters from `/proc` and `statfs`.
- One static Go binary; no Python/Redis/PostgreSQL/Docker dependency.
- Five-second local sampling and healthy-path metric delivery on shared clock boundaries.
- Daily report at 00:00 in the configured MarzWatch timezone.
- TLS certificate fingerprint is pinned inside the Join Key.
- HMAC-signed node metrics.
- Central and agent are systemd resource-limited.
- State snapshots are bounded and flushed at a low frequency to reduce disk writes.

## Install UX
```bash
bash <(curl -fsSL https://raw.githubusercontent.com/SevinEW/marzban-monitoring/main/scripts/install.sh)
```
Then choose:
1. Central Server
2. Node Server

### Central
Prompts for Telegram bot token, admin chat ID, report timezone, port and name. It creates a Join Key after startup.

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
