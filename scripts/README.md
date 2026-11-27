# Scripts

## `quickstart.sh`

Downloads the gummy `.deb` from a GitHub release, verifies it and installs it
with apt. See the [Quickstart](../README.md#-quickstart).

## `run-gummy-on-debian.sh`

Runs gummy in a Debian 13 container for testing and makes it reachable from the
Docker host:

```bash
./scripts/run-gummy-on-debian.sh
```

How it works:

- **Listen address**: gummy listens on `0.0.0.0:8765` inside the container. With
  the default `127.0.0.1`, the published port leads nowhere.
- **Port**: `-p 127.0.0.1:8765:8765` publishes the port on the host's loopback
  only, so it isn't exposed to your network. Use `-p 8765:8765` only if other
  machines need it, and keep in mind that the token travels over plain HTTP.
- **Token**: gummy refuses to listen on a non-loopback address without a token
  (see [Configuration](../README.md#configuration)). The script generates a
  random one inside the container, so it doesn't show up in `docker inspect`.

The setup takes a minute. Run `docker logs -f gummy` until you see
`gummy listening`. Then, on the host:

```bash
TOKEN=$(docker exec gummy cat /etc/gummy/token)
claude mcp add --transport http gummy http://127.0.0.1:8765/mcp \
  --header "Authorization: Bearer $TOKEN"
```

The container has no systemd, so gummy runs as its main process instead of as
`gummy.service`: the container stops when gummy exits. `docker restart gummy`
starts gummy again and keeps the existing installation and token.

To clean up, remove the MCP server from Claude Code and delete the container:

```bash
claude mcp remove gummy
docker rm -f gummy
```
