# Scripts

## `quickstart.sh`

Downloads the gum `.deb` from a GitHub release, verifies it and installs it
with apt. See the [Quickstart](../README.md#-quickstart).

## `run-gum-on-debian.sh`

Runs gum in a Debian 13 container for testing and makes it reachable from the
Docker host:

```bash
./scripts/run-gum-on-debian.sh
```

How it works:

- **Listen address**: gum listens on `0.0.0.0:8765` inside the container. With
  the default `127.0.0.1`, the published port leads nowhere.
- **Port**: `-p 127.0.0.1:8765:8765` publishes the port on the host's loopback
  only, so it isn't exposed to your network. Use `-p 8765:8765` only if other
  machines need it, and keep in mind that the token travels over plain HTTP.
- **Token**: gum refuses to listen on a non-loopback address without a token
  (see [Configuration](../README.md#configuration)). The script generates a
  random one inside the container, so it doesn't show up in `docker inspect`.
- **Hold**: Debian 13 has an unrelated package that's also called `gum`
  (Charmbracelet's gum) with a higher version. The script runs
  `apt-mark hold gum` so that `apt_upgrade` doesn't replace gum with it.

The setup takes a minute. Run `docker logs -f gum` until you see
`gum listening`. Then, on the host:

```bash
TOKEN=$(docker exec gum cat /etc/gum/token)
claude mcp add --transport http gum http://127.0.0.1:8765/mcp \
  --header "Authorization: Bearer $TOKEN"
```

The container has no systemd, so gum runs as its main process instead of as
`gum.service`: the container stops when gum exits. `docker restart gum` starts
gum again and keeps the existing installation and token.

To clean up, remove the MCP server from Claude Code and delete the container:

```bash
claude mcp remove gum
docker rm -f gum
```
