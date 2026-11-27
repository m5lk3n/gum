#!/usr/bin/env bash
# Run gum in a Debian 13 test container, reachable from the Docker host at
# http://127.0.0.1:8765/mcp with a random bearer token. See README.md here.
set -euo pipefail

# The setup runs once; on 'docker restart gum' only gum is started again.
# The token is generated inside the container, so it never shows up in
# 'docker inspect'. Holding gum keeps 'apt_upgrade' from replacing it with
# Debian's unrelated gum package (Charmbracelet's gum).
docker run -d --name gum \
  -p 127.0.0.1:8765:8765 \
  debian:13 bash -c '
    set -euo pipefail
    if [ ! -f /etc/gum/token ]; then
      apt-get update
      apt-get install -y curl ca-certificates
      curl -fsSL https://github.com/m5lk3n/gum/releases/latest/download/quickstart.sh | bash
      apt-mark hold gum
      install -d -m 0750 -o root -g gum /etc/gum
      od -An -tx1 -N32 /dev/urandom | tr -d " \n" |
        install -m 0440 -o root -g gum /dev/stdin /etc/gum/token
    fi
    exec runuser -u gum -- env \
      GUM_LISTEN=0.0.0.0:8765 GUM_TOKEN_FILE=/etc/gum/token /usr/bin/gum
  '

echo "Started. Follow the setup with 'docker logs -f gum' until gum is listening."
