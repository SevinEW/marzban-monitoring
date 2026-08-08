#!/usr/bin/env bash
set -Eeuo pipefail

REPO="${MARZWATCH_REPO:-SevinEW/marzban-monitoring}"
VERSION="${MARZWATCH_VERSION:-latest}"
BIN="/usr/local/bin/marzwatch"
CTL="/usr/local/bin/marzwatchctl"
UNIT="/etc/systemd/system/marzwatch.service"
CREATED_USER=0
CREATED_GROUP=0
INSTALL_STARTED=0
SUCCESS=0
TMP_BIN=""
TMP_SUM=""

C='\033[1;96m'; B='\033[1;94m'; G='\033[1;92m'; Y='\033[1;93m'; R='\033[1;91m'; D='\033[0;90m'; W='\033[1;97m'; N='\033[0m'

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

is_installed() {
  [[ -e "$BIN" || -e "$CTL" || -e /etc/marzwatch || -e /var/lib/marzwatch || -e "$UNIT" ]] && return 0
  systemctl cat marzwatch.service >/dev/null 2>&1
}

backup_existing() {
  local dst="/root/marzwatch-reinstall-backup-$(date +%Y%m%d-%H%M%S)"
  mkdir -p "$dst"
  [[ -f /etc/marzwatch/config.json ]] && cp -a /etc/marzwatch/config.json "$dst/config.json" || true
  [[ -f /var/lib/marzwatch/state.json ]] && cp -a /var/lib/marzwatch/state.json "$dst/state.json" || true
  [[ -f /var/lib/marzwatch/identity.json ]] && cp -a /var/lib/marzwatch/identity.json "$dst/identity.json" || true
  [[ -d /var/lib/marzwatch/tls ]] && cp -a /var/lib/marzwatch/tls "$dst/tls" || true
  [[ -f "$UNIT" ]] && cp -a "$UNIT" "$dst/marzwatch.service" || true
  [[ -f "$BIN" ]] && cp -a "$BIN" "$dst/marzwatch.binary" || true
  chmod -R go-rwx "$dst" 2>/dev/null || true
  printf "%b📦 Backup emergency sakhte shod:%b %s\n" "$Y" "$N" "$dst"
}

clean_marzwatch() {
  printf "%b🧹 Dar hale pak sazi kamel MarzWatch...%b\n" "$Y" "$N"
  systemctl disable --now marzwatch >/dev/null 2>&1 || true
  rm -f "$UNIT" "$BIN" "$CTL" /usr/local/bin/marzwatch.new /usr/local/bin/marzwatch.hud-v2.new
  rm -rf /etc/marzwatch /var/lib/marzwatch
  systemctl daemon-reload >/dev/null 2>&1 || true
  systemctl reset-failed marzwatch >/dev/null 2>&1 || true
  id marzwatch >/dev/null 2>&1 && userdel marzwatch >/dev/null 2>&1 || true
  getent group marzwatch >/dev/null 2>&1 && groupdel marzwatch >/dev/null 2>&1 || true
  printf "%b✅ MarzWatch kamelan pak shod.%b\n" "$G" "$N"
  printf "%b🛡 Marzban / Xray / Docker / Firewall untouched.%b\n" "$D" "$N"
}

safe_repair() {
  printf "\n%b╭━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╮%b\n" "$B" "$N"
  printf "%b┃ 🧰 MARZWATCH SAFE REPAIR%b\n" "$W" "$N"
  printf "%b╰━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╯%b\n" "$B" "$N"

  if [[ ! -x "$BIN" || ! -f /etc/marzwatch/config.json ]]; then
    printf "%b🔴 Binary ya config peyda nashod. Fresh install lazeme.%b\n" "$R" "$N"
    return 1
  fi

  getent group marzwatch >/dev/null || groupadd --system marzwatch
  id marzwatch >/dev/null 2>&1 || useradd --system --gid marzwatch --home-dir /var/lib/marzwatch --shell /usr/sbin/nologin marzwatch
  install -d -m 0750 -o marzwatch -g marzwatch /var/lib/marzwatch
  install -d -m 0750 -o root -g marzwatch /etc/marzwatch
  chown root:marzwatch /etc/marzwatch/config.json
  chmod 0640 /etc/marzwatch/config.json
  [[ -f /var/lib/marzwatch/state.json ]] && chown marzwatch:marzwatch /var/lib/marzwatch/state.json || true
  [[ -f /var/lib/marzwatch/identity.json ]] && { chown marzwatch:marzwatch /var/lib/marzwatch/identity.json; chmod 0600 /var/lib/marzwatch/identity.json; } || true
  chmod 0755 "$BIN"
  ln -sf "$BIN" "$CTL"

  if [[ ! -f "$UNIT" ]]; then
    printf "%b🔴 systemd unit peyda nashod. Fresh install lazeme.%b\n" "$R" "$N"
    return 1
  fi

  systemctl daemon-reload
  systemctl reset-failed marzwatch >/dev/null 2>&1 || true
  systemctl enable marzwatch >/dev/null 2>&1 || true
  systemctl restart marzwatch
  sleep 3

  if systemctl is-active --quiet marzwatch; then
    printf "%b✅ Service repair shod va ONLINE ast.%b\n" "$G" "$N"
    "$CTL" doctor || true
    return 0
  fi

  printf "%b🔴 Repair local kafi nabood. Last logs:%b\n" "$R" "$N"
  journalctl -u marzwatch -n 25 --no-pager || true
  return 1
}

rollback() {
  rc=$?
  [[ -n "$TMP_BIN" ]] && rm -f "$TMP_BIN" || true
  [[ -n "$TMP_SUM" ]] && rm -f "$TMP_SUM" || true
  if [[ $SUCCESS -eq 0 && $INSTALL_STARTED -eq 1 ]]; then
    echo
    printf "%b🧹 Install complete nashod; file haye nasb-e jadid pak mishan...%b\n" "$Y" "$N"
    systemctl disable --now marzwatch >/dev/null 2>&1 || true
    rm -f "$UNIT" "$BIN" "$CTL"
    rm -rf /etc/marzwatch /var/lib/marzwatch
    systemctl daemon-reload >/dev/null 2>&1 || true
    [[ $CREATED_USER -eq 1 ]] && userdel marzwatch >/dev/null 2>&1 || true
    [[ $CREATED_GROUP -eq 1 ]] && groupdel marzwatch >/dev/null 2>&1 || true
    printf "%b✅ Rollback nasb-e jadid anjam shod.%b\n" "$G" "$N"
  fi
  exit "$rc"
}
trap rollback EXIT

[[ ${EUID:-$(id -u)} -eq 0 ]] || { echo "In installer bayad ba root ejra beshe."; exit 1; }
command -v systemctl >/dev/null || { echo "Systemd peyda nashod. In build baraye Linux systemd ast."; exit 1; }

banner
printf "%b╭──────────────────────────────────────╮%b\n" "$B" "$N"
printf "%b│  1) 💠 CENTRAL SERVER               │%b\n" "$W" "$N"
printf "%b│  2) 🛰  NODE SERVER                  │%b\n" "$W" "$N"
printf "%b│  3) 🗑  COMPLETE CLEANUP             │%b\n" "$W" "$N"
printf "%b│  4) 🔐 SHOW CONNECTION TOKEN         │%b\n" "$W" "$N"
printf "%b│  5) 🧰 SAFE REPAIR / SELF-HEAL       │%b\n" "$W" "$N"
printf "%b╰──────────────────────────────────────╯%b\n\n" "$B" "$N"
printf "%bEntekhab kon [1/2/3/4/5]: %b" "$C" "$N"
read -r role
[[ "$role" =~ ^[1-5]$ ]] || { echo "Entekhab namotabar."; exit 1; }

if [[ "$role" == "4" ]]; then
  if [[ ! -x "$BIN" ]]; then
    echo "MarzWatch nasb nist. Token faghat rooye Central nasb-shode namayesh dade mishe."
    SUCCESS=1
    exit 0
  fi
  "$BIN" join-key
  SUCCESS=1
  exit 0
fi

if [[ "$role" == "5" ]]; then
  safe_repair || true
  SUCCESS=1
  exit 0
fi

if [[ "$role" == "3" ]]; then
  if ! is_installed; then
    printf "%bℹ️ MarzWatch rooye in server nasb nist.%b\n" "$Y" "$N"
    SUCCESS=1
    exit 0
  fi
  printf "\n%b⚠️ Faghat MarzWatch pak mishe. Service haye server dast nemikhoran.%b\n" "$Y" "$N"
  printf "Baraye cleanup type kon: DELETE : "
  read -r confirm
  [[ "$confirm" == "DELETE" ]] || { echo "Cleanup cancel shod."; SUCCESS=1; exit 0; }
  backup_existing
  clean_marzwatch
  SUCCESS=1
  printf "\n%b✅ MARZWATCH CLEANUP COMPLETE%b\n" "$G" "$N"
  exit 0
fi

if is_installed; then
  printf "\n%b⚠️ Nasb-e ghabli MarzWatch peyda shod.%b\n" "$Y" "$N"
  printf "%bBaraye nasb Fresh, MarzWatch ghabli kamelan pak mishe.%b\n" "$D" "$N"
  printf "%bGhabl az cleanup yek backup emergency dar /root sakhte mishe.%b\n" "$D" "$N"
  printf "Edame bedam? [y/N]: "
  read -r reinstall
  [[ "${reinstall,,}" == "y" ]] || { echo "Nasb cancel shod."; SUCCESS=1; exit 0; }
  backup_existing
  clean_marzwatch
  echo
fi

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) asset="marzwatch-linux-amd64" ;;
  aarch64|arm64) asset="marzwatch-linux-arm64" ;;
  *) echo "Architecture support nemishe: $arch"; exit 1 ;;
esac

command -v curl >/dev/null || { echo "curl lazeme. Hichi nasb nashod."; exit 1; }
command -v sha256sum >/dev/null || { echo "sha256sum lazeme. Hichi nasb nashod."; exit 1; }

if [[ "$VERSION" == "latest" ]]; then base="https://github.com/${REPO}/releases/latest/download"; else base="https://github.com/${REPO}/releases/${VERSION}/download"; fi
TMP_BIN="$(mktemp)"; TMP_SUM="$(mktemp)"

printf "%b[01/06] 📡 Release jadid dar hale download...%b\n" "$C" "$N"
curl -fL --retry 5 --retry-all-errors --connect-timeout 10 --max-time 120 "$base/$asset" -o "$TMP_BIN"
curl -fL --retry 5 --retry-all-errors --connect-timeout 10 --max-time 60 "$base/SHA256SUMS" -o "$TMP_SUM"
expected="$(awk -v f="$asset" '$2==f {print $1}' "$TMP_SUM")"
actual="$(sha256sum "$TMP_BIN" | awk '{print $1}')"
rm -f "$TMP_SUM"; TMP_SUM=""
[[ -n "$expected" && "$expected" == "$actual" ]] || { printf "%b🔴 SHA256 match nashod. Install stop shod.%b\n" "$R" "$N"; exit 1; }
printf "%b[02/06] 🔐 SHA256 VERIFIED%b\n" "$G" "$N"

INSTALL_STARTED=1
printf "%b[03/06] ⚙️ Core isolated dar hale sakhte shodan...%b\n" "$C" "$N"
install -m 0755 "$TMP_BIN" "$BIN"
ln -sf "$BIN" "$CTL"
if ! getent group marzwatch >/dev/null; then groupadd --system marzwatch; CREATED_GROUP=1; fi
if ! id marzwatch >/dev/null 2>&1; then useradd --system --gid marzwatch --home-dir /var/lib/marzwatch --shell /usr/sbin/nologin marzwatch; CREATED_USER=1; fi
install -d -m 0750 -o marzwatch -g marzwatch /var/lib/marzwatch
install -d -m 0750 -o root -g marzwatch /etc/marzwatch

printf "%b[04/06] 🧩 Setup wizard...%b\n\n" "$C" "$N"
if [[ "$role" == "1" ]]; then "$BIN" setup-central; else "$BIN" setup-agent; fi
chown root:marzwatch /etc/marzwatch/config.json
chmod 0640 /etc/marzwatch/config.json

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

printf "\n%b[05/06] 🚀 Starting MarzWatch...%b\n" "$C" "$N"
systemctl daemon-reload
systemctl enable --now marzwatch
sleep 3
if ! systemctl is-active --quiet marzwatch; then
  printf "%b🟡 Start aval fail shod; Safe Repair automatic ejra mishe...%b\n" "$Y" "$N"
  safe_repair || true
fi
if ! systemctl is-active --quiet marzwatch; then
  printf "%b🔴 MarzWatch ba repair local ham start nashod.%b\n" "$R" "$N"
  journalctl -u marzwatch -n 30 --no-pager || true
  exit 1
fi

printf "%b[06/06] 🩺 Final health check...%b\n" "$C" "$N"
"$CTL" doctor || true

printf "\n%b╔══════════════════════════════════════╗%b\n" "$G" "$N"
printf "%b║      ✅ MARZWATCH CORE ONLINE        ║%b\n" "$G" "$N"
printf "%b╚══════════════════════════════════════╝%b\n" "$G" "$N"

if [[ "$role" == "1" ]]; then
  printf "\n%b🔐 NODE CONNECTION TOKEN%b\n" "$Y" "$N"
  "$BIN" join-key
  printf "%bToken ro private negah dar. Har Node faghat Name + Token mikhad.%b\n" "$D" "$N"
else
  for _ in {1..20}; do
    [[ -s /var/lib/marzwatch/identity.json ]] && break
    sleep 2
  done
  if [[ -s /var/lib/marzwatch/identity.json ]]; then
    printf "%b✅ Node ba Central register shod.%b\n" "$G" "$N"
  else
    printf "%b🟡 Service online ast vali registration retry mishe.%b\n" "$Y" "$N"
    printf "%bNetwork/TLS/Provider error ha auto-retry mishan; firewall/route taghir داده nemishe.%b\n" "$D" "$N"
    echo "Check: journalctl -u marzwatch -f"
  fi
fi

SUCCESS=1
printf "\n%b🛡 Marzban / Xray / Docker / Firewall: UNTOUCHED%b\n" "$G" "$N"
echo "🔐 Token: installer option 4"
echo "🧰 Repair: installer option 5"
printf "%b⚡ Powered by Only :)%b\n" "$D" "$N"
