# gummy

<!-- markdownlint-disable MD033 -->
<h2 align="center"><img src="images/gummy.png" alt="Logo" width="250"><pre><small>An MCP server for apt-get {update|upgrade|autoremove}</small><br/><s><i>g</i>o-<i>u</i>til-<i>m</i>cp</s></pre></h2>

<p align="center">
<img alt="Go badge" src="https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go&logoColor=white"/>
<img alt="License badge app" src="https://img.shields.io/badge/License-MIT-green"/>
</p>

For now, a very<sup>3</sup> simple utility MCP server written in Go that
performs a very<sup>3</sup> limited set of APT tasks on Debian-based
hosts.
<!-- markdownlint-enable MD033 -->

## 🚀 Quickstart

On a Debian-based host (amd64 or arm64) with systemd, run:

```sh
curl -fsSL \
  https://github.com/m5lk3n/gummy/releases/latest/download/quickstart.sh \
  | sudo bash
```

This downloads the latest `.deb` package, verifies its checksum and installs it
with `apt-get` (see [Installation](#-installation)). gummy then listens on
`127.0.0.1:8765`. To pin a version, run `... | sudo GUMMY_VERSION=1.2.3 bash`.

Once installed, follow [Configuration](#configuration) and [Connecting a client](#connecting-a-client).

## ✨ Features

gummy runs one apt job at a time. An apt call made while another job is running
fails immediately with "another apt job is already running".

### apt

`apt_update` (a.k.a. `apt-get update`):

- "Retrieve new lists of packages"
- Refreshes package index
- Returns the number of packages that can be upgraded (`upgradable`) and the
  number that `apt_upgrade` would keep back (`kept_back`)
- Throttled: if the index was refreshed less than `GUMMY_UPDATE_MIN_INTERVAL` ago,
  the network refresh is skipped (`refreshed: false`) and the counts are
  recomputed from the existing index
- Synchronous call

`apt_upgrade` (a.k.a. `apt-get upgrade`):

- "Perform an upgrade"
- Upgrades those installed packages reported by `apt_update` that are actually
  "upgradable", i.e., not being kept back
- Without `confirm`, only previews the upgrade (`apt-get -s upgrade`, see
  [Previews](#previews))
- With `confirm` set to `true`, upgrades (`apt-get upgrade -y`)
- Keeps locally modified configuration files (`--force-confdef --force-confold`)
- Returns the names of the packages that are (or would be) upgraded
  (`upgraded`) and kept back (`kept_back`)
- **Potentially long-running call** when confirmed, set `async` to `true` to
  skip waiting

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `confirm` | boolean | no | `false` | Upgrade for real; otherwise only preview the upgrade |
| `async` | boolean | no | `false` | Don't wait: start the job and return immediately; requires `confirm` |

`apt_autoremove` (a.k.a. `apt-get autoremove`):

- "Remove automatically all unused packages"
- Remove unused dependencies
- Without `confirm`, only previews the removal (`apt-get -s autoremove`, see
  [Previews](#previews))
- With `confirm` set to `true`, removes them (`apt-get autoremove -y`)
- Returns the names of the packages that are (or would be) removed (`removed`)
- **Potentially long-running call** when confirmed, set `async` to `true` to
  skip waiting

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `confirm` | boolean | no | `false` | Remove for real; otherwise only preview the removal |
| `async` | boolean | no | `false` | Don't wait: start the job and return immediately; requires `confirm` |

`apt_status`:

- Reports the job currently running (`running`), if any
- Reports the outcome of the most recent `update`, `upgrade` and `autoremove`
  job (`last`): state (`succeeded` or `failed`), start and finish time,
  duration, and the result or error
- Reports when the package index was last refreshed (`last_refresh`) and when
  `apt_update` will refresh it again (`next_refresh`)
- Doesn't run apt; state is kept in memory and reset when gummy restarts
- Synchronous call

### General

`version`:

- Returns the version of the running gummy, who built it (`built_by`) and the Go
  version it was built with
- Reports `restart_pending: true` if a newer gummy binary was installed since gummy
  started, e.g., by `apt_upgrade` (see [Upgrading](#upgrading))
- Synchronous call

### Previews

Like `apt-get upgrade` without `-y`, which lists the packages and asks before
it changes anything, `apt_upgrade` and `apt_autoremove` only preview their job
unless `confirm` is `true`. A preview simulates the job as the unprivileged
`gummy` user, changes nothing, and returns
`{"status": "preview", "summary": {...}, ...}` with the package names. A typical
upgrade is:

1. `apt_update` to refresh the package index
2. `apt_upgrade` to see which packages would be upgraded
3. `apt_upgrade` with `confirm: true` (and optionally `async: true`) to upgrade

A preview is quick and always synchronous, so `async: true` without
`confirm: true` is rejected. A preview counts as an apt job, so it fails while
another job is running, but `apt_status` doesn't record it. The confirmed job
may differ from the preview if the package index was refreshed in between; its
result lists the packages it actually changed.

### Async calls

`async: true` (together with `confirm: true`) is fire and forget: the call
returns `{"status": "started"}` as soon as the job has started. Poll
`apt_status` to see whether the job is still running and how it ended. The
result is also written to the service log (`journalctl -u gummy`).

A synchronous call returns `{"status": "completed", "summary": {...}}`. If the
client disconnects or gives up waiting, the job still runs to completion. gummy
never interrupts dpkg because a client went away.

### Errors

Failures are returned as tool errors and include apt-get's exit code and the
last 40 lines of its output. gummy recognizes these cases:

- Another apt job is running in gummy (the error names the job and when it
  started)
- The apt/dpkg lock is held by another process, e.g., `unattended-upgrades`
- dpkg was interrupted: run `dpkg --configure -a` as root, because gummy doesn't
  repair this
- sudo refused to run apt-get, usually because `/etc/sudoers.d/gummy` is missing
  or outdated
- The job exceeded `GUMMY_JOB_TIMEOUT`

## 🏗 Installation

gummy runs as a systemd service under an unprivileged `gummy` user. It reaches root
only through a sudoers rule that allows exactly the three `apt-get` command
lines gummy uses (`gummy --print-sudoers gummy` prints it).

Each [release](https://github.com/m5lk3n/gummy/releases) provides
`gummy_linux_amd64.deb` and `gummy_linux_arm64.deb`, with checksums in
`SHA256SUMS`. Instead of the [Quickstart](#-quickstart), you can download the
package yourself and install it:

```sh
sudo apt install ./gummy_linux_amd64.deb
```

The package installs:

- `/usr/bin/gummy`
- `/usr/lib/systemd/system/gummy.service`, which is enabled and started
- `/etc/default/gummy`, a configuration file that upgrades keep
- `/etc/needrestart/conf.d/gummy.conf` (see [Upgrading](#upgrading))
- the `gummy` system user
- `/etc/sudoers.d/gummy`, generated by `gummy --print-sudoers` and validated with
  `visudo` on every install and upgrade

For a detailed breakdown, see [Bill of Material](BOM.md).

### Uninstalling

The Quickstart installs gummy as a regular Debian package, so remove it with apt,
whichever way you installed it:

```sh
# stop gummy.service, remove the binary, the unit and the sudoers rule:
sudo apt remove gummy
# or, to also remove /etc/default/gummy and the systemd state:
sudo apt purge gummy
```

Even `purge` leaves the following in place. Remove them manually if needed:

- the `gummy` system user, which is kept as is usual for Debian packages:
  `sudo deluser --system gummy`
- `/etc/gummy/token`, if you set a token (see [Configuration](#configuration)),
  because no package owns it: `sudo rm -rf /etc/gummy`
- the MCP client entry, if you added one (see
  [Connecting a client](#connecting-a-client)), e.g., `claude mcp remove gummy`,
  or delete the entry from your `.mcp.json`

### Upgrading

Install a newer package, rerun the Quickstart, or let gummy upgrade itself with
`apt_upgrade` (`confirm: true`) if it's in one of your apt sources. An upgrade
never stops or restarts gummy: when gummy upgrades itself, apt-get runs inside
`gummy.service`, and restarting the service from there would deadlock until the
stop timeout and then kill dpkg. For the same reason, the package tells
`needrestart` to leave gummy alone. The previous version keeps running until you
restart it, and the `version` tool reports `restart_pending: true` in the
meantime:

```sh
sudo systemctl restart gummy
```

The sudoers rule is updated right away. If a release changes the `apt-get`
calls gummy makes, the old process may be refused by sudo until you restart gummy.

### Configuration

Set these in `/etc/default/gummy`, or pass them as flags:

| Variable | Flag | Default | Meaning |
| --- | --- | --- | --- |
| `GUMMY_LISTEN` | `--listen` | `127.0.0.1:8765` | Address of the streamable HTTP endpoint `/mcp` |
| `GUMMY_TOKEN_FILE` | `--token-file` | none | File holding the bearer token; required for non-loopback addresses |
| `GUMMY_UPDATE_MIN_INTERVAL` | `--update-min-interval` | `15m` | Minimum time between package index refreshes |
| `GUMMY_JOB_TIMEOUT` | `--job-timeout` | `60m` | Maximum duration of a single apt job |

If `GUMMY_LISTEN` isn't a loopback address, gummy refuses to start without a token.
To set a token:

```sh
sudo install -d -m 0750 -o root -g gummy /etc/gummy
openssl rand -hex 32 | sudo install -m 0440 -o root -g gummy /dev/stdin /etc/gummy/token
echo 'GUMMY_TOKEN_FILE=/etc/gummy/token' | sudo tee -a /etc/default/gummy
sudo systemctl restart gummy
```

Clients then send `Authorization: Bearer <token>`. Plain HTTP carries the token
in clear text, so put a TLS-terminating reverse proxy in front of gummy, or tunnel
over SSH. Don't expose gummy on an untrusted network.

### Connecting a client

For example, with Claude Code:

```sh
claude mcp add --transport http gummy http://127.0.0.1:8765/mcp
# with a token:
claude mcp add --transport http gummy http://host:8765/mcp \
  --header "Authorization: Bearer $TOKEN"
```

Clients that use a JSON config file (e.g. `.mcp.json`) take an entry like
this:

```json
{
  "mcpServers": {
    "gummy": {
      "type": "http",
      "url": "http://127.0.0.1:8765/mcp",
      "headers": {
        "Authorization": "Bearer <token>"
      }
    }
  }
}
```

Omit `headers` when no token is configured.

## 🛠 Development

### Prerequisites

- `make`
- `go` 1.27+
- `docker` 29+
- optional:
  - `golangci-lint` and `govulncheck` for `make check-security`
  - `rumdl` for `make lint-md` and `make fmt-md`
  - [todo.txt-cli](https://github.com/todotxt/todo.txt-cli) with
    `alias t='todo.sh -d ./todo.cfg'`

### Common tasks

```sh
make build           # build ./gummy
make test            # run unit tests
make check-security  # golangci-lint (incl. gosec) and govulncheck
make lint-md         # lint Markdown with rumdl
make package         # build snapshot .deb packages into dist/ (GoReleaser in Docker)
```

To install a local build, run `make package`, then
`sudo apt install ./dist/gummy_linux_amd64.deb`.

### Releasing

Push a tag like `v1.2.3`. The `release` workflow runs GoReleaser
(`.goreleaser.yaml`), which runs the tests, builds the packages and creates the
GitHub release with the `.deb` packages, binary archives, `quickstart.sh` and
`SHA256SUMS`. The asset names contain no version, so
`releases/latest/download/<name>` always points to the latest release.

The package's maintainer scripts and files are in `packaging/`.

The code is split into the following parts:

- `internal/apt`: the apt-get calls, throttling, the one-job-at-a-time lock and
  error classification. Tests use a fake runner, so they don't need apt.
- `internal/server`: MCP tool registration and bearer-token middleware.
- `main.go`: configuration, the HTTP server and graceful shutdown. On SIGTERM,
  gummy waits for a running apt job to finish.

The `apt-get` argument lists in `internal/apt/runner.go` are the single source
for both the commands gummy runs and the generated sudoers rule. If you change
them, install the new package to update `/etc/sudoers.d/gummy`.

## ⚖️ Disclaimer

gummy lets an MCP client, and so an AI agent, change the packages installed on a
host as root. Upgrades can restart services, change configuration and, rarely,
break a system. Use gummy only on hosts where that's acceptable, keep backups, and
review what your agent does. The software is provided "as is", without warranty
of any kind; see [LICENSE](LICENSE).

## 📄 Acknowledgements

- Inspired by [apt-mcp](https://github.com/brianlechthaler/apt-mcp/).
- Claude Code implemented this project.
- Several GenAIs helped me to create the logo :innocent:

---

<!-- markdownlint-disable MD033 -->
<sup>3</sup>) very, very, very

---

<p align="center">
    <a href="https://lttl.dev/"><img alt="lttl.dev logo" src="images/lttl.dev.png"/></a>
</p>
