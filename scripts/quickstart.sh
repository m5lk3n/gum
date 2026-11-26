#!/usr/bin/env bash
# Download the gum .deb from a GitHub release, verify it and install it with apt.
# Usage: curl -fsSL https://github.com/m5lk3n/gum/releases/latest/download/quickstart.sh | sudo bash
# Pin a version: ... | sudo GUM_VERSION=1.2.3 bash
# Running it again upgrades gum; restart gum.service afterwards to activate it.
# Remove gum with 'sudo apt remove gum' ('purge' also removes /etc/default/gum).
set -euo pipefail

REPO=m5lk3n/gum

die() { echo "quickstart.sh: $*" >&2; exit 1; }

# Everything runs inside main, so a truncated download executes nothing.
main() {
    [[ $EUID -eq 0 ]] || die "must run as root, e.g., '... | sudo bash'"
    command -v apt-get >/dev/null || die "apt-get not found; gum only supports Debian-based systems"
    command -v curl >/dev/null || die "curl not found"
    command -v sha256sum >/dev/null || die "sha256sum not found"

    local arch
    arch="$(dpkg --print-architecture)"
    case "$arch" in
        amd64 | arm64) ;;
        *) die "unsupported architecture '$arch' (supported: amd64, arm64)" ;;
    esac

    local base
    if [[ -n "${GUM_VERSION:-}" ]]; then
        base="https://github.com/$REPO/releases/download/v${GUM_VERSION#v}"
    else
        base="https://github.com/$REPO/releases/latest/download"
    fi

    local deb="gum_linux_${arch}.deb"
    local tmp
    tmp="$(mktemp -d)"
    # shellcheck disable=SC2064 # expand $tmp now
    trap "rm -rf '$tmp'" EXIT
    # apt's sandbox user (_apt) must be able to read the local .deb.
    chmod 0755 "$tmp"

    echo "Downloading $base/$deb"
    curl -fsSL --proto '=https' -o "$tmp/$deb" "$base/$deb"
    curl -fsSL --proto '=https' -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"

    echo "Verifying checksum"
    (cd "$tmp" && grep " $deb\$" SHA256SUMS | sha256sum -c --quiet -) ||
        die "checksum verification failed"
    chmod 0644 "$tmp/$deb"

    DEBIAN_FRONTEND=noninteractive apt-get install -y "$tmp/$deb"
}

main "$@"
