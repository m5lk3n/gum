#!/usr/bin/env bash
# Install gum as a systemd service on a Debian-based host.
# Usage: sudo scripts/install.sh [path/to/gum-binary]
set -euo pipefail

BIN_SRC="${1:-./gum}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN=/usr/local/bin/gum
USER_NAME=gum
SUDOERS=/etc/sudoers.d/gum
DEFAULTS=/etc/default/gum
UNIT=/etc/systemd/system/gum.service

die() { echo "install.sh: $*" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "must run as root"
[[ -x /usr/bin/apt-get ]] || die "/usr/bin/apt-get not found; gum only supports apt-based systems"
[[ -x /usr/bin/sudo ]] || die "/usr/bin/sudo not found; install sudo first"
command -v visudo >/dev/null || die "visudo not found"
[[ -f "$BIN_SRC" ]] || die "binary '$BIN_SRC' not found; run 'make build' first or pass its path"

echo "Installing binary to $BIN"
install -m 0755 "$BIN_SRC" "$BIN"

if ! id -u "$USER_NAME" >/dev/null 2>&1; then
    echo "Creating system user $USER_NAME"
    useradd --system --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin "$USER_NAME"
fi

echo "Installing sudoers rule to $SUDOERS"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
"$BIN" --print-sudoers "$USER_NAME" >"$tmp"
visudo -cf "$tmp" >/dev/null || die "generated sudoers rule failed validation; not installed"
install -m 0440 -o root -g root "$tmp" "$SUDOERS"

if [[ ! -e "$DEFAULTS" ]]; then
    echo "Creating $DEFAULTS"
    install -m 0644 /dev/stdin "$DEFAULTS" <<'CFG'
# gum configuration, read by gum.service.
#GUM_LISTEN=127.0.0.1:8765
# File holding the bearer token; required for non-loopback addresses.
# Make it readable by the gum user only: install -m 0400 -o gum -g gum token /etc/gum/token
#GUM_TOKEN_FILE=/etc/gum/token
#GUM_UPDATE_MIN_INTERVAL=15m
#GUM_JOB_TIMEOUT=60m
CFG
else
    echo "Keeping existing $DEFAULTS"
fi

echo "Installing systemd unit to $UNIT"
install -m 0644 "$SCRIPT_DIR/gum.service" "$UNIT"

if [[ -d /run/systemd/system ]]; then
    systemctl daemon-reload
    systemctl enable gum.service
    systemctl restart gum.service
    echo "gum is running: $(systemctl is-active gum.service)"
else
    echo "systemd is not running; start gum manually: sudo -u $USER_NAME $BIN"
fi
