#!/usr/bin/env bash
set -Eeuo pipefail

REPO="${MARZWATCH_REPO:-SevinEW/marzban-monitoring}"
VERSION="${MARZWATCH_VERSION:-latest}"
BIN="/usr/local/bin/marzwatch"
CTL="/usr/local/bin/marzwatchctl"
UNIT="/etc/systemd/system/marzwatch.service"
CONFIG="/etc/marzwatch/config.json"
STATE_DIR="/var/lib/marzwatch"
UPDATER_DIR="/usr/local/lib/marzwatch"
UPDATER="$UPDATER_DIR/auto-update.sh"
UPDATE_SERVICE="/etc/systemd/system/marzwatch-auto-update.service"
UPDATE_TIMER="/etc/systemd/system/marzwatch-auto-update.timer"
CENTRAL_PORT="28443"

C='\033[1;96m'
B='\033[1;94m'
G='\033[1;92m'
Y='\033[1;93m'
R='\033[1;91m'
D='\033[0;90m'
W='\033[1;97m'
N='\033[0m'

TMP_BIN=""
TMP_SUM=""

cleanup_tmp() {
  [[ -n "${TMP_BIN:-}" ]] && rm -f "$TMP_BIN" || true
  [[ -n "${TMP_SUM:-}" ]] && rm -f "$TMP_SUM" || true
}
trap cleanup_tmp EXIT

banner() {
  clear 2>/dev/null || true
  printf "%b" "$C"
  cat <<'EOF'

          ███╗   ███╗ █████╗ ██████╗ ███████╗
          ████╗ ████║██╔══██╗██╔══██╗╚══███╔╝
          ██╔████╔██║███████║██████╔╝  ███╔╝
          ██║╚██╔╝██║██╔══██║██╔══██╗ ███╔╝
          ██║ ╚═╝ ██║██║  ██║██║  ██║███████╗
          ╚═╝     ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝

             W  A  T  C  H   //   C  O  R  E
        ╔══════════════════════════════════════╗
        ║   INFRASTRUCTURE MONITORING SYSTEM  ║
        ╚══════════════════════════════════════╝
EOF
  printf "%b\n" "$N"
  printf "%b      🛡 SAFE MODE • SERVER SERVICES PROTECTED%b\n" "$G" "$N"
  printf "%b      Marzban / Xray / Docker / Firewall dast nemikhoran.%b\n\n" "$D" "$N"
}

require_root() {
  [[ ${EUID:-$(id -u)} -eq 0 ]] || { echo "In installer bayad ba root ejra beshe."; exit 1; }
  command -v systemctl >/dev/null || { echo "Systemd peyda nashod. In build baraye Linux systemd ast."; exit 1; }
}

is_installed() {
  [[ -e "$BIN" || -e "$CTL" || -e "$CONFIG" || -e "$STATE_DIR" || -e "$UNIT" ]] && return 0
  systemctl cat marzwatch.service >/dev/null 2>&1
}

detect_asset() {
  case "$(uname -m)" in
    x86_64|amd64) ASSET="marzwatch-linux-amd64" ;;
    aarch64|arm64) ASSET="marzwatch-linux-arm64" ;;
    *) echo "Architecture support nemishe: $(uname -m)"; return 1 ;;
  esac
}

release_base() {
  if [[ "$VERSION" == "latest" ]]; then
    printf 'https://github.com/%s/releases/latest/download' "$REPO"
  else
    printf 'https://github.com/%s/releases/%s/download' "$REPO" "$VERSION"
  fi
}

download_verified_binary() {
  detect_asset
  command -v curl >/dev/null || { echo "curl lazeme."; return 1; }
  command -v sha256sum >/dev/null || { echo "sha256sum lazeme."; return 1; }

  local base expected actual
  base="$(release_base)"
  TMP_BIN="$(mktemp)"
  TMP_SUM="$(mktemp)"

  printf "%b📡 Downloading latest verified release...%b\n" "$C" "$N"
  curl -fL --retry 3 --connect-timeout 10 --max-time 120 "$base/$ASSET" -o "$TMP_BIN"
  curl -fL --retry 3 --connect-timeout 10 --max-time 60 "$base/SHA256SUMS" -o "$TMP_SUM"

  expected="$(awk -v f="$ASSET" '$2==f {print $1}' "$TMP_SUM")"
  actual="$(sha256sum "$TMP_BIN" | awk '{print $1}')"

  if [[ -z "$expected" || "$expected" != "$actual" ]]; then
    printf "%b🔴 SHA256 verification failed. Hich taghiri anjam nashod.%b\n" "$R" "$N"
    return 1
  fi

  NEW_SHA="$actual"
  printf "%b✅ SHA256 VERIFIED%b  %s\n" "$G" "$N" "$NEW_SHA"
}

bootstrap_auto_updater() {
  command -v curl >/dev/null || { echo "curl lazeme."; return 1; }
  command -v sha256sum >/dev/null || { echo "sha256sum lazeme."; return 1; }

  local base tmpdir expected actual
  base="$(release_base)"
  tmpdir="$(mktemp -d)"

  printf "%b🤖 Auto-Updater dar hale bootstrap...%b\n" "$C" "$N"
  if ! curl -fL --retry 3 --connect-timeout 10 --max-time 60 "$base/marzwatch-auto-update.sh" -o "$tmpdir/marzwatch-auto-update.sh"; then
    rm -rf "$tmpdir"
    printf "%b🔴 Auto-Updater download nashod.%b\n" "$R" "$N"
    return 1
  fi
  if ! curl -fL --retry 3 --connect-timeout 10 --max-time 60 "$base/SHA256SUMS" -o "$tmpdir/SHA256SUMS"; then
    rm -rf "$tmpdir"
    return 1
  fi

  expected="$(awk '$2=="marzwatch-auto-update.sh" {print $1}' "$tmpdir/SHA256SUMS")"
  actual="$(sha256sum "$tmpdir/marzwatch-auto-update.sh" | awk '{print $1}')"
  if [[ -z "$expected" || "$expected" != "$actual" ]]; then
    rm -rf "$tmpdir"
    printf "%b🔴 Auto-Updater SHA256 match nashod.%b\n" "$R" "$N"
    return 1
  fi

  install -d -m 0700 "$UPDATER_DIR"
  install -m 0750 "$tmpdir/marzwatch-auto-update.sh" "$UPDATER.new"
  mv "$UPDATER.new" "$UPDATER"
  rm -rf "$tmpdir"

  "$UPDATER" --force
  printf "%b✅ AUTO UPDATE ENABLED%b\n" "$G" "$N"
  printf "%b   Latest release hourly check mishe; Fleet signal update ro sari tar trigger mikone.%b\n" "$D" "$N"
}

write_unit() {
  cat > "$UNIT" <<'UNIT'
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
UNIT
}

ensure_runtime_layout() {
  getent group marzwatch >/dev/null || groupadd --system marzwatch
  id marzwatch >/dev/null 2>&1 || useradd --system --gid marzwatch --home-dir "$STATE_DIR" --shell /usr/sbin/nologin marzwatch

  install -d -m 0750 -o marzwatch -g marzwatch "$STATE_DIR"
  install -d -m 0750 -o root -g marzwatch /etc/marzwatch

  [[ -f "$CONFIG" ]] && chown root:marzwatch "$CONFIG" || true
  [[ -f "$CONFIG" ]] && chmod 0640 "$CONFIG" || true
  [[ -f "$STATE_DIR/state.json" ]] && chown marzwatch:marzwatch "$STATE_DIR/state.json" || true
  [[ -f "$STATE_DIR/identity.json" ]] && chown marzwatch:marzwatch "$STATE_DIR/identity.json" || true
  [[ -f "$STATE_DIR/identity.json" ]] && chmod 0600 "$STATE_DIR/identity.json" || true
  [[ -d "$STATE_DIR/tls" ]] && chown -R marzwatch:marzwatch "$STATE_DIR/tls" || true

  [[ -x "$BIN" ]] && ln -sfn "$BIN" "$CTL" || true
}

backup_existing() {
  local dst="/root/marzwatch-reinstall-backup-$(date +%Y%m%d-%H%M%S)"
  mkdir -m 0700 -p "$dst"
  [[ -f "$CONFIG" ]] && cp -a "$CONFIG" "$dst/config.json" || true
  [[ -f "$STATE_DIR/state.json" ]] && cp -a "$STATE_DIR/state.json" "$dst/state.json" || true
  [[ -f "$STATE_DIR/identity.json" ]] && cp -a "$STATE_DIR/identity.json" "$dst/identity.json" || true
  [[ -d "$STATE_DIR/tls" ]] && cp -a "$STATE_DIR/tls" "$dst/tls" || true
  [[ -f "$UNIT" ]] && cp -a "$UNIT" "$dst/marzwatch.service" || true
  [[ -f "$BIN" ]] && cp -a "$BIN" "$dst/marzwatch.binary" || true
  [[ -f "$UPDATER" ]] && cp -a "$UPDATER" "$dst/auto-update.sh" || true
  printf "%b📦 Emergency backup:%b %s\n" "$Y" "$N" "$dst"
}

clean_marzwatch() {
  printf "%b🧹 Dar hale pak sazi faghat MarzWatch...%b\n" "$Y" "$N"
  systemctl disable --now marzwatch-auto-update.timer >/dev/null 2>&1 || true
  systemctl stop marzwatch-auto-update.service >/dev/null 2>&1 || true
  systemctl disable --now marzwatch >/dev/null 2>&1 || true
  rm -f "$UPDATE_TIMER" "$UPDATE_SERVICE" "$UNIT" "$BIN" "$CTL" /usr/local/bin/marzwatch.new
  rm -rf "$UPDATER_DIR" /var/lib/marzwatch-updater /etc/marzwatch "$STATE_DIR"
  systemctl daemon-reload >/dev/null 2>&1 || true
  systemctl reset-failed marzwatch marzwatch-auto-update.service >/dev/null 2>&1 || true
  id marzwatch >/dev/null 2>&1 && userdel marzwatch >/dev/null 2>&1 || true
  getent group marzwatch >/dev/null 2>&1 && groupdel marzwatch >/dev/null 2>&1 || true
  printf "%b✅ MarzWatch kamelan pak shod.%b\n" "$G" "$N"
  printf "%b🛡 Marzban / Xray / Docker / Firewall untouched.%b\n" "$D" "$N"
}

safe_repair() {
  if ! is_installed || [[ ! -x "$BIN" || ! -f "$CONFIG" ]]; then
    printf "%b🔴 Nasb-e kamel MarzWatch peyda nashod. Az option 1 ya 2 estefade kon.%b\n" "$R" "$N"
    return 1
  fi

  printf "%b🧰 Safe Repair dar hale barresi MarzWatch...%b\n" "$C" "$N"
  ensure_runtime_layout
  write_unit
  systemctl daemon-reload
  systemctl enable marzwatch >/dev/null 2>&1 || true
  systemctl restart marzwatch
  sleep 3

  if systemctl is-active --quiet marzwatch; then
    printf "%b✅ Service repair shod va ONLINE ast.%b\n" "$G" "$N"
    "$CTL" doctor || true
    if [[ ! -x "$UPDATER" || ! -f "$UPDATE_TIMER" ]]; then
      bootstrap_auto_updater || true
    fi
    return 0
  fi

  printf "%b🔴 Repair local kafi nabood. Last logs:%b\n" "$R" "$N"
  journalctl -u marzwatch -n 30 --no-pager || true
  return 1
}

fresh_install() {
  local role="$1"

  if is_installed; then
    printf "\n%b⚠️ Nasb-e ghabli MarzWatch peyda shod.%b\n" "$Y" "$N"
    printf "%bFresh install, data haye MarzWatch ghabli ro reset mikone.%b\n" "$D" "$N"
    printf "%bBaraye update bedoon hazf data az option 6 estefade kon.%b\n" "$G" "$N"
    printf "Edame bedam? [y/N]: "
    read -r reinstall
    [[ "${reinstall,,}" == "y" ]] || { echo "Nasb cancel shod."; return 0; }
    backup_existing
    clean_marzwatch
  fi

  printf "%b[01/06] 📡 Fetch latest release...%b\n" "$C" "$N"
  download_verified_binary
  printf "%b[02/06] 🔐 Release verified%b\n" "$G" "$N"

  printf "%b[03/06] ⚙️ Building isolated runtime...%b\n" "$C" "$N"
  install -m 0755 "$TMP_BIN" "$BIN"
  ensure_runtime_layout

  printf "%b[04/06] 🧩 Setup wizard...%b\n\n" "$C" "$N"
  if [[ "$role" == "1" ]]; then
    "$BIN" setup-central
  else
    "$BIN" setup-agent
  fi
  ensure_runtime_layout
  write_unit

  printf "\n%b[05/06] 🚀 Starting MarzWatch...%b\n" "$C" "$N"
  systemctl daemon-reload
  systemctl enable --now marzwatch
  sleep 3

  if ! systemctl is-active --quiet marzwatch; then
    printf "%b🔴 MarzWatch start nashod.%b\n" "$R" "$N"
    journalctl -u marzwatch -n 30 --no-pager || true
    return 1
  fi

  printf "%b[06/06] 🤖 Enabling safe automatic updates...%b\n" "$C" "$N"
  bootstrap_auto_updater

  printf "\n%b╔══════════════════════════════════════╗%b\n" "$G" "$N"
  printf "%b║      ✅ MARZWATCH CORE ONLINE        ║%b\n" "$G" "$N"
  printf "%b╚══════════════════════════════════════╝%b\n" "$G" "$N"

  if [[ "$role" == "1" ]]; then
    printf "\n%b🔐 NODE CONNECTION TOKEN%b\n" "$Y" "$N"
    "$BIN" join-key
    printf "%bToken ro private negah dar.%b\n" "$D" "$N"
  else
    for _ in {1..15}; do
      [[ -s "$STATE_DIR/identity.json" ]] && break
      sleep 2
    done
    if [[ -s "$STATE_DIR/identity.json" ]]; then
      printf "%b✅ Node ba Central register shod.%b\n" "$G" "$N"
    else
      printf "%b🟡 Service online ast vali registration hanooz retry mishe.%b\n" "$Y" "$N"
      echo "Safe diagnostics: installer ro dobare ejra kon va option 5 ro bezan."
    fi
  fi

  printf "\n%b🛡 Marzban / Xray / Docker / Firewall: UNTOUCHED%b\n" "$G" "$N"
  echo "🩺 Check: marzwatchctl doctor"
  echo "🤖 Auto Update: ENABLED"
  printf "%b⚡ Powered by Only :)%b\n" "$D" "$N"
}

require_root
banner

printf "%b╭────────────────────────────────────────╮%b\n" "$B" "$N"
printf "%b│  1) 💠 CENTRAL SERVER                 │%b\n" "$W" "$N"
printf "%b│  2) 🛰  NODE SERVER                    │%b\n" "$W" "$N"
printf "%b│  3) 🗑  COMPLETE CLEANUP               │%b\n" "$W" "$N"
printf "%b│  4) 🔐 SHOW CONNECTION TOKEN           │%b\n" "$W" "$N"
printf "%b│  5) 🧰 SAFE REPAIR / SELF-HEAL         │%b\n" "$W" "$N"
printf "%b│  6) 🚀 UPDATE NOW + UPDATE ALL NODES   │%b\n" "$W" "$N"
printf "%b╰────────────────────────────────────────╯%b\n\n" "$B" "$N"
printf "%bEntekhab kon [1/2/3/4/5/6]: %b" "$C" "$N"
read -r choice
[[ "$choice" =~ ^[1-6]$ ]] || { echo "Entekhab namotabar."; exit 1; }

case "$choice" in
  1|2)
    fresh_install "$choice"
    ;;
  3)
    if ! is_installed; then
      printf "%bℹ️ MarzWatch rooye in server nasb nist.%b\n" "$Y" "$N"
      exit 0
    fi
    printf "\n%b⚠️ Faghat MarzWatch pak mishe. Service haye asli server dast nemikhoran.%b\n" "$Y" "$N"
    printf "Baraye cleanup type kon: DELETE : "
    read -r confirm
    [[ "$confirm" == "DELETE" ]] || { echo "Cleanup cancel shod."; exit 0; }
    backup_existing
    clean_marzwatch
    ;;
  4)
    if [[ ! -x "$BIN" ]]; then
      echo "MarzWatch nasb nist."
      exit 0
    fi
    "$BIN" join-key
    ;;
  5)
    safe_repair
    ;;
  6)
    if ! is_installed || [[ ! -x "$BIN" || ! -f "$CONFIG" ]]; then
      printf "%b🔴 MarzWatch nasb-shode peyda nashod.%b\n" "$R" "$N"
      exit 1
    fi
    bootstrap_auto_updater
    ;;
esac
