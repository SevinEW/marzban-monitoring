#!/usr/bin/env bash
set -Eeuo pipefail
[[ ${EUID:-$(id -u)} -eq 0 ]] || { echo "Run as root"; exit 1; }
if [[ -x /usr/local/bin/marzwatch ]]; then exec /usr/local/bin/marzwatch uninstall; fi
echo "MarzWatch binary not found."
