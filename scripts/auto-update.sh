#!/usr/bin/env bash
set -Eeuo pipefail

REPO="${MARZWATCH_REPO:-SevinEW/marzban-monitoring}"
VERSION="${MARZWATCH_VERSION:-latest}"
BIN="/usr/local/bin/marzwatch"
CTL="/usr/local/bin/marzwatchctl"
CONFIG="/etc/marzwatch/config.json"
STATE_DIR="/var/lib/marzwatch"
REQUEST="$STATE_DIR/update.request"
UPDATER_DIR="/usr/local/lib/marzwatch"
UPDATER="$UPDATER_DIR/auto-update.sh"
UNIT="/etc/systemd/system/marzwatch.service"
UPDATE_SERVICE="/etc/systemd/system/marzwatch-auto-update.service"
UPDATE_TIMER="/etc/systemd/system/marzwatch-auto-update.timer"
STAMP_DIR="/var/lib/marzwatch-updater"
BACKUP_DIR="$STAMP_DIR/backups"
LAST_CHECK="$STAMP_DIR/last-check"
LOCK_FILE="/run/marzwatch-auto-update.lock"
CENTRAL_PORT="28443"
FORCE=0
[[ "${1:-}" == "--force" ]] && FORCE=1

log() { printf '[MarzWatch Update] %s\n' "$*"; }
fail() { log "ERROR: $*"; exit 1; }

[[ ${EUID:-$(id -u)} -eq 0 ]] || fail "root required"
[[ -x "$BIN" && -f "$CONFIG" ]] || exit 0
command -v curl >/dev/null || fail "curl missing"
command -v sha256sum >/dev/null || fail "sha256sum missing"
command -v systemctl >/dev/null || fail "systemd missing"

mkdir -p "$STAMP_DIR" "$BACKUP_DIR" "$UPDATER_DIR"
chmod 0700 "$STAMP_DIR" "$BACKUP_DIR" "$UPDATER_DIR"

if command -v flock >/dev/null 2>&1; then
  exec 9>"$LOCK_FILE"
  flock -n 9 || exit 0
fi

if [[ -f "$REQUEST" ]]; then
  FORCE=1
fi

if [[ $FORCE -eq 0 && -f "$LAST_CHECK" ]]; then
  now="$(date +%s)"
  last="$(stat -c %Y "$LAST_CHECK" 2>/dev/null || echo 0)"
  if (( now - last < 3600 )); then
    exit 0
  fi
fi

touch "$LAST_CHECK"
chmod 0600 "$LAST_CHECK"

case "$(uname -m)" in
  x86_64|amd64) asset="marzwatch-linux-amd64" ;;
  aarch64|arm64) asset="marzwatch-linux-arm64" ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

if [[ "$VERSION" == "latest" ]]; then
  base="https://github.com/${REPO}/releases/latest/download"
else
  base="https://github.com/${REPO}/releases/${VERSION}/download"
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

log "checking verified release"
curl -fL --retry 3 --connect-timeout 10 --max-time 120 "$base/$asset" -o "$tmpdir/$asset"
curl -fL --retry 3 --connect-timeout 10 --max-time 60 "$base/marzwatch-auto-update.sh" -o "$tmpdir/marzwatch-auto-update.sh"
curl -fL --retry 3 --connect-timeout 10 --max-time 60 "$base/SHA256SUMS" -o "$tmpdir/SHA256SUMS"

verify_asset() {
  local name="$1" expected actual
  expected="$(awk -v f="$name" '$2==f {print $1}' "$tmpdir/SHA256SUMS")"
  [[ -n "$expected" ]] || fail "checksum missing for $name"
  actual="$(sha256sum "$tmpdir/$name" | awk '{print $1}')"
  [[ "$expected" == "$actual" ]] || fail "checksum mismatch for $name"
}
verify_asset "$asset"
verify_asset "marzwatch-auto-update.sh"

new_sha="$(sha256sum "$tmpdir/$asset" | awk '{print $1}')"
old_sha="$(sha256sum "$BIN" | awk '{print $1}')"
new_updater_sha="$(sha256sum "$tmpdir/marzwatch-auto-update.sh" | awk '{print $1}')"
old_updater_sha="$(sha256sum "$UPDATER" 2>/dev/null | awk '{print $1}' || true)"

backup="$BACKUP_DIR/$(date +%Y%m%d-%H%M%S)"
mkdir -m 0700 -p "$backup"
cp -a "$BIN" "$backup/marzwatch.binary"
[[ -f "$UNIT" ]] && cp -a "$UNIT" "$backup/marzwatch.service" || true
[[ -f "$UPDATER" ]] && cp -a "$UPDATER" "$backup/auto-update.sh" || true

# Retain only the newest five updater backups.
find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' 2>/dev/null \
  | sort -nr \
  | awk 'NR>5 {$1=""; sub(/^ /,""); print}' \
  | while IFS= read -r old; do [[ -n "$old" ]] && rm -rf -- "$old"; done

write_main_unit() {
  cat > "$UNIT.new" <<'UNITEOF'
[Unit]
Description=MarzWatch Lightweight Infrastructure Monitor
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=simple
User=marzwatch
Group=marzwatch
ExecStart=/usr/local/bin/marzwatch run
Restart=on-failure
RestartSec=7s
Nice=10
CPUQuota=20%
MemoryMax=128M
TasksMax=64
LimitNOFILE=2048
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
LockPersonality=true
RestrictSUIDSGID=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
ReadWritePaths=/var/lib/marzwatch

[Install]
WantedBy=multi-user.target
UNITEOF
  chmod 0644 "$UNIT.new"
}

write_update_units() {
  cat > "$UPDATE_SERVICE" <<'EOF'
[Unit]
Description=MarzWatch Safe Automatic Updater
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
User=root
ExecStart=/usr/local/lib/marzwatch/auto-update.sh
Nice=15
IOSchedulingClass=idle
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=full
ReadWritePaths=/usr/local/bin /usr/local/lib/marzwatch /etc/systemd/system /var/lib/marzwatch /var/lib/marzwatch-updater /run
EOF

  cat > "$UPDATE_TIMER" <<'EOF'
[Unit]
Description=MarzWatch Automatic Update Check

[Timer]
OnBootSec=4min
OnUnitActiveSec=2min
RandomizedDelaySec=30s
Persistent=true
Unit=marzwatch-auto-update.service

[Install]
WantedBy=timers.target
EOF
  chmod 0644 "$UPDATE_SERVICE" "$UPDATE_TIMER"
}

unit_changed=0
write_main_unit
if [[ ! -f "$UNIT" ]] || ! cmp -s "$UNIT.new" "$UNIT"; then
  unit_changed=1
  mv "$UNIT.new" "$UNIT"
else
  rm -f "$UNIT.new"
fi

if [[ "$new_updater_sha" != "$old_updater_sha" ]]; then
  install -m 0750 "$tmpdir/marzwatch-auto-update.sh" "$UPDATER.new"
  mv "$UPDATER.new" "$UPDATER"
fi
write_update_units
systemctl daemon-reload
systemctl enable --now marzwatch-auto-update.timer >/dev/null 2>&1 || true

binary_changed=0
if [[ "$new_sha" != "$old_sha" ]]; then
  binary_changed=1
  log "new build found; applying atomically"
  install -m 0755 "$tmpdir/$asset" /usr/local/bin/marzwatch.new
  mv /usr/local/bin/marzwatch.new "$BIN"
  ln -sfn "$BIN" "$CTL"
fi

if [[ $binary_changed -eq 1 || $unit_changed -eq 1 ]]; then
  systemctl restart marzwatch
  sleep 4
  if ! systemctl is-active --quiet marzwatch; then
    log "new build failed; automatic rollback"
    cp -a "$backup/marzwatch.binary" "$BIN"
    chmod 0755 "$BIN"
    ln -sfn "$BIN" "$CTL"
    if [[ -f "$backup/marzwatch.service" ]]; then
      cp -a "$backup/marzwatch.service" "$UNIT"
    fi
    if [[ -f "$backup/auto-update.sh" ]]; then
      cp -a "$backup/auto-update.sh" "$UPDATER"
      chmod 0750 "$UPDATER"
    fi
    systemctl daemon-reload
    systemctl restart marzwatch || true
    exit 1
  fi

  role="$(grep -oE '"role"[[:space:]]*:[[:space:]]*"(central|agent)"' "$CONFIG" | head -n1 | sed -E 's/.*"(central|agent)"/\1/' || true)"
  if [[ "$role" == "central" ]]; then
    if ! curl -kfsS --connect-timeout 3 --max-time 7 "https://127.0.0.1:${CENTRAL_PORT}/healthz" >/dev/null; then
      log "central health check failed; automatic rollback"
      cp -a "$backup/marzwatch.binary" "$BIN"
      chmod 0755 "$BIN"
      ln -sfn "$BIN" "$CTL"
      [[ -f "$backup/marzwatch.service" ]] && cp -a "$backup/marzwatch.service" "$UNIT" || true
      systemctl daemon-reload
      systemctl restart marzwatch || true
      exit 1
    fi
    if [[ $binary_changed -eq 1 ]]; then
      "$CTL" fleet-update >/dev/null 2>&1 || true
      log "fleet update signal sent to nodes"
    fi
  fi
fi

rm -f "$REQUEST"
log "up to date • binary ${new_sha:0:12}"
