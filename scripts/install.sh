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

rollback() {
  rc=$?
  [[ -n "$TMP_BIN" ]] && rm -f "$TMP_BIN" || true
  [[ -n "$TMP_SUM" ]] && rm -f "$TMP_SUM" || true
  if [[ $SUCCESS -eq 0 && $INSTALL_STARTED -eq 1 ]]; then
    echo
    echo "🧹 Rolling back incomplete MarzWatch installation..."
    systemctl disable --now marzwatch >/dev/null 2>&1 || true
    rm -f "$UNIT" "$BIN" "$CTL"
    rm -rf /etc/marzwatch /var/lib/marzwatch
    systemctl daemon-reload >/dev/null 2>&1 || true
    [[ $CREATED_USER -eq 1 ]] && userdel marzwatch >/dev/null 2>&1 || true
    [[ $CREATED_GROUP -eq 1 ]] && groupdel marzwatch >/dev/null 2>&1 || true
    echo "✅ Rollback complete. Marzban/Xray/Docker/Firewall were not changed."
  fi
  exit "$rc"
}
trap rollback EXIT

[[ ${EUID:-$(id -u)} -eq 0 ]] || { echo "Run as root."; exit 1; }

if [[ -e "$BIN" || -e /etc/marzwatch/config.json || -e "$UNIT" ]]; then
  echo "🟡 MarzWatch appears to be already installed."
  echo "Run: marzwatchctl doctor"
  exit 1
fi

echo "╭━━━━━━━━━━━━━━━━━━━━━━━━━━━━╮"
echo "┃   💠 MARZWATCH INSTALLER    ┃"
echo "┃ Safe Infrastructure Monitor ┃"
echo "╰━━━━━━━━━━━━━━━━━━━━━━━━━━━━╯"
echo
echo "🛡 SAFE MODE • ACTIVE"
echo "  ✅ Marzban config untouched"
echo "  ✅ Xray config untouched"
echo "  ✅ Docker untouched"
echo "  ✅ Firewall untouched"
echo "  ✅ DNS / Routes untouched"
echo
printf "1) 💠 Central Server\n2) 🛰 Node Server\n\nSelect role [1/2]: "
read -r role
[[ "$role" == "1" || "$role" == "2" ]] || { echo "Invalid selection"; exit 1; }

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) asset="marzwatch-linux-amd64" ;;
  aarch64|arm64) asset="marzwatch-linux-arm64" ;;
  *) echo "Unsupported architecture: $arch"; exit 1 ;;
esac

command -v curl >/dev/null || { echo "curl is required. Nothing was installed."; exit 1; }
command -v sha256sum >/dev/null || { echo "sha256sum is required. Nothing was installed."; exit 1; }

if [[ "$VERSION" == "latest" ]]; then
  base="https://github.com/${REPO}/releases/latest/download"
else
  base="https://github.com/${REPO}/releases/${VERSION}/download"
fi
TMP_BIN="$(mktemp)"
TMP_SUM="$(mktemp)"

printf "\n📦 Downloading verified MarzWatch binary...\n"
curl -fL --retry 3 --connect-timeout 10 --max-time 120 "$base/$asset" -o "$TMP_BIN"
curl -fL --retry 3 --connect-timeout 10 --max-time 60 "$base/SHA256SUMS" -o "$TMP_SUM"
expected="$(awk -v f="$asset" '$2==f {print $1}' "$TMP_SUM")"
actual="$(sha256sum "$TMP_BIN" | awk '{print $1}')"
rm -f "$TMP_SUM"
TMP_SUM=""
[[ -n "$expected" && "$expected" == "$actual" ]] || { echo "🔴 SHA256 verification failed. Nothing was installed."; exit 1; }
echo "✅ SHA256 verified"

if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
  echo "🟡 UFW is active. MarzWatch will NOT modify it."
  echo "   Ensure the selected Central port is reachable from your nodes."
fi

INSTALL_STARTED=1
install -m 0755 "$TMP_BIN" "$BIN"
ln -sf "$BIN" "$CTL"

if ! getent group marzwatch >/dev/null; then groupadd --system marzwatch; CREATED_GROUP=1; fi
if ! id marzwatch >/dev/null 2>&1; then useradd --system --gid marzwatch --home-dir /var/lib/marzwatch --shell /usr/sbin/nologin marzwatch; CREATED_USER=1; fi
install -d -m 0750 -o marzwatch -g marzwatch /var/lib/marzwatch
install -d -m 0750 -o root -g marzwatch /etc/marzwatch

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

systemctl daemon-reload
systemctl enable --now marzwatch
sleep 3
if ! systemctl is-active --quiet marzwatch; then
  echo "🔴 MarzWatch did not start."
  journalctl -u marzwatch -n 30 --no-pager || true
  exit 1
fi

printf "\n✅ MarzWatch is running.\n"
if [[ "$role" == "1" ]]; then
  printf "\n🔐 JOIN KEY\n"
  "$BIN" join-key
else
  for _ in {1..15}; do
    [[ -s /var/lib/marzwatch/identity.json ]] && break
    sleep 2
  done
  if [[ -s /var/lib/marzwatch/identity.json ]]; then
    echo "✅ Node registered with Central"
  else
    echo "🟡 Node service is running but registration is still retrying."
    echo "Check: journalctl -u marzwatch -f"
  fi
fi

SUCCESS=1
printf "\n🛡 Existing Marzban/Xray/Docker/Firewall configuration: UNTOUCHED\n"
echo "🩺 Health check: marzwatchctl doctor"
echo "🗑 Clean removal: marzwatchctl uninstall"
