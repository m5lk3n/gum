# GUM

<!-- markdownlint-disable MD033 -->
<h1 align="center"><img src="images/gum.png" alt="Logo"><pre><small>go-util-mcp</small></pre></h1>

<p align="center">
<img alt="Go badge" src="https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go&logoColor=white"/>
<img alt="License badge app" src="https://img.shields.io/badge/License-MIT-green"/>
</p>

For now, a very<sup>3</sup> simple utility MCP server written in Go that
performs a very<sup>3</sup> limited set of Linux admin tasks on Debian-based
hosts.
<!-- markdownlint-enable MD033 -->

## 🚀 Quickstart

TODO

## ✨ Features

gum runs one apt job at a time. An apt call made while another job is running
fails immediately with "another apt job is already running".

### apt

`apt_update` (a.k.a. `apt-get update`):

- "Retrieve new lists of packages"
- Refreshes package index
- Returns the number of packages that can be upgraded (`upgradable`) and the
  number that `apt_upgrade` would keep back (`kept_back`)
- Throttled: if the index was refreshed less than `GUM_UPDATE_MIN_INTERVAL` ago,
  the network refresh is skipped (`refreshed: false`) and the counts are
  recomputed from the existing index
- Synchronous call

`apt_upgrade` (a.k.a. `apt-get upgrade -y`):

- "Perform an upgrade"
- Upgrades those installed packages reported by `apt_update` that are actually
  "upgradable", i.e., not being kept back
- Keeps locally modified configuration files (`--force-confdef --force-confold`)
- **Potentially long-running call**, set `async` to `true` to skip waiting

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `async` | boolean | no | `false` | Don't wait: start the job and return immediately |

`apt_autoremove` (a.k.a. `apt-get autoremove -y`):

- "Remove automatically all unused packages"
- Remove unused dependencies
- **Potentially long-running call**, set `async` to `true` to skip waiting

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `async` | boolean | no | `false` | Don't wait: start the job and return immediately |

`apt_status`:

- Reports the job currently running (`running`), if any
- Reports the outcome of the most recent `update`, `upgrade` and `autoremove`
  job (`last`): state (`succeeded` or `failed`), start and finish time,
  duration, and the result or error
- Reports when the package index was last refreshed (`last_refresh`) and when
  `apt_update` will refresh it again (`next_refresh`)
- Doesn't run apt; state is kept in memory and reset when gum restarts
- Synchronous call

### General

`version`:

- Returns the gum version, who built it (`built_by`) and the Go version it was
  built with
- Synchronous call

### Async calls

`async: true` is fire and forget: the call returns `{"status": "started"}` as
soon as the job has started. Poll `apt_status` to see whether the job is still
running and how it ended. The result is also written to the service log
(`journalctl -u gum`).

A synchronous call returns `{"status": "completed", "summary": {...}}`. If the
client disconnects or gives up waiting, the job still runs to completion. gum
never interrupts dpkg because a client went away.

### Errors

Failures are returned as tool errors and include apt-get's exit code and the
last 40 lines of its output. gum recognizes these cases:

- Another apt job is running in gum (the error names the job and when it
  started)
- The apt/dpkg lock is held by another process, e.g., `unattended-upgrades`
- dpkg was interrupted: run `dpkg --configure -a` as root, because gum doesn't
  repair this
- sudo refused to run apt-get, usually because `/etc/sudoers.d/gum` is missing
  or outdated
- The job exceeded `GUM_JOB_TIMEOUT`

## 🏗 Installation

gum runs as a systemd service under an unprivileged `gum` user. It reaches root
only through a sudoers rule that allows exactly the three `apt-get` command
lines gum uses (`gum --print-sudoers gum` prints it).

```sh
make build
sudo scripts/install.sh ./gum
```

`install.sh` installs these files:

- `/usr/local/bin/gum`
- the `gum` system user
- `/etc/sudoers.d/gum` (validated with `visudo`)
- `/etc/default/gum`, which is never overwritten
- `/etc/systemd/system/gum.service`, which is then enabled and started

To remove gum, run `sudo scripts/uninstall.sh`. Add `--purge` to remove
`/etc/default/gum` too.

### Configuration

Set these in `/etc/default/gum`, or pass them as flags:

| Variable | Flag | Default | Meaning |
| --- | --- | --- | --- |
| `GUM_LISTEN` | `--listen` | `127.0.0.1:8765` | Address of the streamable HTTP endpoint `/mcp` |
| `GUM_TOKEN_FILE` | `--token-file` | none | File holding the bearer token; required for non-loopback addresses |
| `GUM_UPDATE_MIN_INTERVAL` | `--update-min-interval` | `15m` | Minimum time between package index refreshes |
| `GUM_JOB_TIMEOUT` | `--job-timeout` | `60m` | Maximum duration of a single apt job |

If `GUM_LISTEN` isn't a loopback address, gum refuses to start without a token.
To set a token:

```sh
sudo install -d -m 0750 -o root -g gum /etc/gum
openssl rand -hex 32 | sudo install -m 0440 -o root -g gum /dev/stdin /etc/gum/token
echo 'GUM_TOKEN_FILE=/etc/gum/token' | sudo tee -a /etc/default/gum
sudo systemctl restart gum
```

Clients then send `Authorization: Bearer <token>`. Plain HTTP carries the token
in clear text, so put a TLS-terminating reverse proxy in front of gum, or tunnel
over SSH. Don't expose gum on an untrusted network.

### Connecting a client

For example, with Claude Code:

```sh
claude mcp add --transport http gum http://127.0.0.1:8765/mcp
# with a token:
claude mcp add --transport http gum http://host:8765/mcp \
  --header "Authorization: Bearer $TOKEN"
```

## 🛠 Development

### Prerequisites

- `make`
- `go` 1.27+
- optional: [todo.txt-cli](https://github.com/todotxt/todo.txt-cli) with
  `alias t='todo.sh -d ./todo.cfg'`

### Common tasks

```sh
make build           # build ./gum
make test            # run unit tests
make check-security  # golangci-lint (incl. gosec) and govulncheck
make lint-md         # lint Markdown with rumdl
```

The code is split into the following parts:

- `internal/apt`: the apt-get calls, throttling, the one-job-at-a-time lock and
  error classification. Tests use a fake runner, so they don't need apt.
- `internal/server`: MCP tool registration and bearer-token middleware.
- `main.go`: configuration, the HTTP server and graceful shutdown. On SIGTERM,
  gum waits for a running apt job to finish.

The `apt-get` argument lists in `internal/apt/runner.go` are the single source
for both the commands gum runs and the generated sudoers rule. If you change
them, reinstall to update `/etc/sudoers.d/gum`.

## ※ Disclaimer

gum lets an MCP client, and so an AI agent, change the packages installed on a
host as root. Upgrades can restart services, change configuration and, rarely,
break a system. Use gum only on hosts where that's acceptable, keep backups, and
review what your agent does. The software is provided "as is", without warranty
of any kind; see [LICENSE](LICENSE).

## 📄 Acknowledgements

- Inspired by [apt-mcp](https://github.com/brianlechthaler/apt-mcp/).
- Claude Code implemented this project.
- Several GenAIs helped me to create the logo :-)
