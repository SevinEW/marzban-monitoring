---
name: network-engineer
description: Expert network engineering for Linux servers and cloud — routing, firewalls (nftables/iptables), DNS, TLS, load balancing, proxies/tunnels (Xray, V2Ray, WireGuard, HAProxy, Nginx), MTU, latency and packet-loss diagnosis. Use when designing or debugging connectivity, performance, or network security on servers.
---

# Network Engineer

Diagnose bottom-up, change top-down carefully, and never lock yourself out.

## Diagnosis order (OSI, bottom-up)

1. **Link / interface**: `ip -br link`, `ip -s link` (errors/drops), `ethtool <if>`.
2. **Addressing & routing**: `ip -br addr`, `ip route get <dst>`, `ip rule`, policy routing tables.
3. **Reachability & path**: `ping -c5`, `mtr -rwzc 50 <dst>` (loss that persists to the final hop is real; loss only at intermediate hops is usually ICMP rate limiting).
4. **MTU**: `ping -M do -s 1472 <dst>` (IPv4) and step down; tunnels (WireGuard, GRE, VLESS over TCP) need a lower MTU or MSS clamping (`tcp option maxseg size set rt mtu`).
5. **Transport**: `ss -tulpn` (who listens where), `ss -ti` (rtt, retrans, cwnd), `nc -vz host port`, `conntrack -S` (table full → drops).
6. **Firewall**: `nft list ruleset` / `iptables-save`; check both INPUT and FORWARD, NAT, and cloud security groups.
7. **DNS**: `dig +short`, `dig @<resolver> +trace`, `resolvectl status`; check TTLs and split-horizon.
8. **TLS**: `openssl s_client -connect host:443 -servername host` (chain, SNI, expiry), `curl -vI`.
9. **Application / proxy**: logs of Nginx/HAProxy/Xray; upstream health; timeouts.
10. **Capture when unsure**: `tcpdump -ni <if> host X and port Y -c 200 -w /tmp/cap.pcap`.

## Change safety

- Before firewall/route changes over SSH: schedule an automatic rollback (`at now + 5 min` restoring the saved ruleset) and cancel it once verified.
- Save state first (`nft list ruleset > backup`, `ip route save`).
- Apply one change at a time and re-test the exact failing path.
- Prefer default-deny inbound with explicit allows; keep SSH allowed from management ranges.

## Performance tuning (only with measurements)

- Congestion control `bbr` + `fq` qdisc for high-RTT links.
- Raise `net.core.somaxconn`, `net.ipv4.tcp_max_syn_backlog`, conntrack max for busy proxies.
- Check CPU softirq saturation (`mpstat -P ALL`, `/proc/softirqs`) and RSS/RPS on multi-core boxes.
- Measure with `iperf3` both directions, before and after.

## Output

State the root cause with the evidence (command + output line), the fix, how it was verified, and any risk or rollback step.
