---
name: sast-configuration
description: Configure Static Application Security Testing (SAST) and secret/dependency scanning — Semgrep, CodeQL, gosec, Bandit, govulncheck, pip-audit, gitleaks, Trivy — locally and in GitHub Actions, with tuned rules and baselines. Use when adding or fixing security scanning in a repo or CI pipeline.
---

# SAST Configuration

## 1. Detect the stack

Look at `go.mod`, `pyproject.toml`/`requirements*.txt`, `package.json`, Dockerfiles, shell scripts, IaC. Pick tools per stack:

| Stack | Code | Dependencies |
|---|---|---|
| Go | `gosec`, Semgrep `p/golang`, CodeQL go | `govulncheck` |
| Python | `bandit`, Semgrep `p/python`, CodeQL python | `pip-audit` |
| JS/TS | Semgrep `p/javascript`, CodeQL js | `npm audit` |
| Shell | `shellcheck` | – |
| Docker/IaC | `hadolint`, `trivy config` | `trivy fs` |
| All | `gitleaks` (secrets) | – |

## 2. Run locally first

Run each tool, read the findings, and triage: true positive → fix or open an issue; false positive → suppress narrowly with a reason (`# nosec G104 -- reason`, `# nosemgrep: rule-id`), never by disabling the whole rule set.

## 3. CI (GitHub Actions) template

```yaml
name: security
on:
  pull_request:
  push: { branches: [main] }
  schedule: [ { cron: "17 3 * * 1" } ]
permissions:
  contents: read
  security-events: write
jobs:
  semgrep:
    runs-on: ubuntu-latest
    container: semgrep/semgrep
    steps:
      - uses: actions/checkout@v4
      - run: semgrep scan --config p/default --sarif -o semgrep.sarif --error
      - uses: github/codeql-action/upload-sarif@v3
        if: always()
        with: { sarif_file: semgrep.sarif }
  secrets:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: gitleaks/gitleaks-action@v2
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}   # required to scan pull_request events
          # GITLEAKS_LICENSE: ${{ secrets.GITLEAKS_LICENSE }}  # required for repos owned by an organization
    permissions:
      contents: read
      pull-requests: read   # lets the action list the PR's commits
```

Pin action versions (ideally to SHAs), keep `permissions` minimal, and add the per-language job (e.g. `go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./...`).

`--error` fails the job on any finding. On a repo with existing findings, either drop it during the report-only phase or scan only what the PR adds with `--baseline-commit ${{ github.event.pull_request.base.sha }}` (pull_request events only).

## 4. Rollout policy

- Start in report-only mode on existing code; establish a baseline (Semgrep `--baseline-commit`, gitleaks `.gitleaksignore`).
- Fail PRs only on **new** high/critical findings.
- Document suppressions and review them periodically.
- Add a `SECURITY.md` and enable Dependabot / secret scanning where available.
