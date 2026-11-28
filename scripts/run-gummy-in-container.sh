#!/usr/bin/env bash
# Run gummy in a Debian 13 test container, reachable from the Docker host at
# http://127.0.0.1:8765/mcp with a random bearer token. See README.md here.
set -euo pipefail

# The setup runs once; on 'docker restart gummy' only gummy is started again.
# The token is generated inside the container, so it never shows up in
# 'docker inspect'.
docker run -d --name gummy \
  -p 127.0.0.1:8765:8765 \
  debian:13 bash -c '
    set -euo pipefail
    if [ ! -f /etc/gummy/token ]; then
      apt-get update
      apt-get install -y curl ca-certificates
      curl -fsSL https://github.com/m5lk3n/gummy/releases/latest/download/quickstart.sh | bash
      install -d -m 0750 -o root -g gummy /etc/gummy
      od -An -tx1 -N32 /dev/urandom | tr -d " \n" |
        install -m 0440 -o root -g gummy /dev/stdin /etc/gummy/token
    fi
    exec runuser -u gummy -- env \
      GUMMY_LISTEN=0.0.0.0:8765 GUMMY_TOKEN_FILE=/etc/gummy/token /usr/bin/gummy
  '

echo "Started. Follow the setup with 'docker logs -f gummy' until gummy is listening."
