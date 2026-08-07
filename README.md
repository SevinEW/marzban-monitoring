# 💠 MarzWatch

Lightweight, fail-safe monitoring for Marzban/Xray node fleets with Telegram reporting.

## Design rules
- Does **not** modify Marzban, Xray, Docker, firewall, routes, DNS or certificates.
- Agent reads lightweight Linux counters from `/proc` and `statfs`.
- One static Go binary; no Python/Redis/PostgreSQL/Docker dependency.
- 15-second local sampling, ~60-second metric delivery, 10-minute Telegram summary.
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
- Telegram 10-minute dashboard
- CPU/RAM/Disk sustained alerts + recovery
- Offline / recovered node alerts
- Daily traffic and average summary

## Safety
The installer never executes `apt upgrade`, `docker prune`, `iptables`, `ufw`, `sysctl`, or service restarts for Xray/Marzban.
