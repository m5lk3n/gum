#!/usr/bin/env bash
# Remove gum installed by install.sh.
# Usage: sudo scripts/uninstall.sh [--purge]   (--purge also removes /etc/default/gum)
set -euo pipefail

PURGE=false
if [[ "${1:-}" == "--purge" ]]; then
    PURGE=true
fi

die() { echo "uninstall.sh: $*" >&2; exit 1; }
[[ $EUID -eq 0 ]] || die "must run as root"

if [[ -d /run/systemd/system ]] && systemctl list-unit-files gum.service >/dev/null 2>&1; then
    echo "Stopping gum (waits for a running apt job to finish)"
    systemctl disable --now gum.service || true
fi

rm -f /etc/systemd/system/gum.service
if [[ -d /run/systemd/system ]]; then
    systemctl daemon-reload
fi
rm -f /etc/sudoers.d/gum /usr/local/bin/gum

if id -u gum >/dev/null 2>&1; then
    echo "Removing system user gum"
    userdel gum
fi

if $PURGE; then
    rm -f /etc/default/gum
else
    if [[ -e /etc/default/gum ]]; then
        echo "Kept /etc/default/gum (use --purge to remove it)"
    fi
fi
echo "gum uninstalled"
