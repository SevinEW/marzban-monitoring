---
name: grafana-dashboards
description: Create, review and maintain production Grafana dashboards as code (JSON models, provisioning, Grafonnet/Jsonnet). Use when building or fixing a Grafana dashboard, panel, variable, alert rule or provisioning file, or when turning existing metrics into a dashboard.
---

# Grafana Dashboards

Build dashboards that answer a question an operator has at 3 a.m., kept in git and provisioned, never hand-edited only in the UI.

## Workflow

1. **Inventory the metrics first.** Find what is actually exported (Prometheus `/metrics`, exporter docs, recording rules, or the code that emits them). Never invent metric names; if one is missing, say so and propose the instrumentation.
2. **Decide the dashboard's job.** One audience per dashboard: fleet overview, single-node drill-down, or service internals. Link between them with dashboard links / data links rather than cramming everything into one.
3. **Lay out top to bottom by urgency:**
   - Row 1: health at a glance — stat panels for up/down, error rate, saturation, with thresholds (green/amber/red).
   - Row 2: the golden signals over time — latency (p50/p95/p99 via `histogram_quantile`), traffic, errors, saturation (USE for resources: utilisation, saturation, errors).
   - Lower rows (collapsed): per-instance breakdowns, internals, logs panel.
4. **Template it.** Add `datasource` (type `prometheus`), `job`, `instance`/`node` variables with `label_values(...)`, multi-select + "All" where it makes sense. Every query uses the variables.
5. **Write correct PromQL.**
   - Counters: always `rate()`/`increase()` over `$__rate_interval`, never raw.
   - Aggregate with `sum by (...)` before `histogram_quantile(0.95, sum by (le, ...)(rate(x_bucket[$__rate_interval])))`.
   - Ratios: guard divide-by-zero by filtering the denominator (`a / (b > 0)`); don't use `clamp_min(b, 1)`, which distorts any rate below 1/s.
   - Avoid high-cardinality labels in legends; set `legendFormat` (`{{instance}}`).
6. **Set units and thresholds** on every panel (`bytes`, `bps`, `percentunit`, `s`, `reqps`). Min 0 for counts. Decimals sensible.
7. **Provision as code.** Store the JSON under the repo (e.g. `grafana/dashboards/*.json`) with a provisioning file:
   ```yaml
   apiVersion: 1
   providers:
     - name: default
       folder: <Folder>
       type: file
       allowUiUpdates: false
       options: { path: /var/lib/grafana/dashboards }
   ```
   Keep a stable `uid`, set `"id": null`, bump `version` only via Grafana or tooling, and remove `__inputs`/`__requires` noise unless exporting for grafana.com.
8. **Alerts** (Grafana-managed or Prometheus rules): alert on symptoms (error rate, latency SLO burn, node down for N minutes) with `for:` to avoid flapping; include a summary, a runbook link and the dashboard link in annotations.

## Review checklist

- [ ] Every metric name exists; queries return data for at least one real series.
- [ ] No raw counters; `$__rate_interval` used; no `irate` on panels with long ranges.
- [ ] Units, thresholds, legends set; no "Panel Title" left over.
- [ ] Variables drive all queries; "All" doesn't explode cardinality.
- [ ] Dashboard JSON validates (`jq . file.json`) and has a stable `uid`.
- [ ] Time zone choice is explicit if the business runs on a specific zone (e.g. `Asia/Tehran`).
