---
name: prometheus-configuration
description: Set up and review Prometheus for metric collection — scrape configs, service discovery, relabeling, recording and alerting rules, Alertmanager routing, retention and instrumentation of Go/Python services. Use when writing prometheus.yml, rule files, alertmanager.yml, exporters, or adding /metrics to an app.
---

# Prometheus Configuration

## Workflow

1. **Map the targets.** List every process to scrape, its port and path, and whether it is reachable directly, via an exporter (node_exporter, blackbox_exporter, cadvisor) or via a Pushgateway (only for batch jobs).
2. **Write `prometheus.yml`:**
   ```yaml
   global:
     scrape_interval: 15s
     evaluation_interval: 15s
     external_labels: { cluster: <name> }
   rule_files: [ "rules/*.yml" ]
   alerting:
     alertmanagers: [ { static_configs: [ { targets: [ "alertmanager:9093" ] } ] } ]
   scrape_configs:
     - job_name: node
       static_configs: [ { targets: [ "10.0.0.1:9100" ], labels: { node: edge-1 } } ]
   ```
   Prefer file_sd / consul / kubernetes SD over long static lists. Use `relabel_configs` to set stable, low-cardinality labels and `metric_relabel_configs` to drop noisy series.
3. **Secure scraping.** Expose `/metrics` on a private interface or behind TLS/basic auth (`scheme: https`, `tls_config`, `basic_auth`/`authorization`). Never put secrets inline — use `*_file` fields.
4. **Recording rules** for expensive or repeated queries, named `level:metric:operations` (e.g. `job:http_requests:rate5m`).
5. **Alerting rules** — symptom-based, with `for:` and useful annotations:
   ```yaml
   groups:
     - name: availability
       rules:
         - alert: TargetDown
           expr: up == 0
           for: 3m
           labels: { severity: critical }
           annotations:
             summary: "{{ $labels.instance }} is down"
             runbook_url: https://...
   ```
   Include `absent()` alerts for metrics that must exist, and disk-fill prediction with `predict_linear`.
6. **Alertmanager**: route by `severity`/`team`, `group_by: [alertname, cluster]`, sensible `group_wait`/`repeat_interval`, inhibit warnings when the matching critical fires. Telegram/Slack/webhook receivers read tokens from files.
7. **Retention & resources**: set `--storage.tsdb.retention.time` / `.size`; estimate memory from active series (`prometheus_tsdb_head_series`). Use remote_write only if long-term storage is needed.

## Instrumenting code

- Counters end in `_total`, durations in `_seconds` (histograms), sizes in `_bytes`. Base units only.
- Labels must be bounded — never user IDs, IPs of clients, emails or raw URLs.
- Go: `prometheus/client_golang` with a custom registry; Python: `prometheus_client`.
- Expose build info (`<app>_build_info{version="..."} 1`).

## Validate before shipping

```bash
promtool check config prometheus.yml
promtool check rules rules/*.yml
promtool test rules tests/*.yml     # unit-test alert rules
amtool check-config alertmanager.yml
```
