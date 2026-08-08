from pathlib import Path

p = Path("internal/central/server.go")
s = p.read_text()

const_anchor = 'const keyPath = "/var/lib/marzwatch/tls/server.key"\n'
if 'const fleetUpdatePath = "/var/lib/marzwatch/fleet-update.request"' not in s:
    s = s.replace(
        const_anchor,
        const_anchor + 'const fleetUpdatePath = "/var/lib/marzwatch/fleet-update.request"\n',
        1,
    )

old = '''\tupdated, _ := s.store.UpdateMetric(id, m, s.tz)\n\ts.evaluate(updated)\n\tw.WriteHeader(204)\n'''
new = '''\tupdated, _ := s.store.UpdateMetric(id, m, s.tz)\n\ts.evaluate(updated)\n\tif b, err := os.ReadFile(fleetUpdatePath); err == nil {\n\t\tif fleetTS, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil && fleetTS > 0 && time.Since(time.Unix(fleetTS, 0)) < 15*time.Minute {\n\t\t\tw.Header().Set("X-Marzwatch-Fleet-Update", strconv.FormatInt(fleetTS, 10))\n\t\t}\n\t}\n\tw.WriteHeader(204)\n'''
if old not in s:
    raise SystemExit("metrics response anchor not found")
s = s.replace(old, new, 1)

if '"os"\n' not in s:
    s = s.replace('"net/http"\n', '"net/http"\n\t"os"\n', 1)

p.write_text(s)
