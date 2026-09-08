# dashsync

A CLI tool that turns running Docker containers into static config files for
self-hosted dashboards — [Homepage](https://gethomepage.dev) and
[Homer](https://github.com/bastienwirtz/homer) today, Dashy planned — syncs
them idempotently, and doesn't clobber whatever you edited by hand.

> **Status: pre-1.0, functional.** Docker label discovery works, single- or
> multi-host. Homepage gets the full idempotent merge; Homer is render-only
> for now (see [docs/decisions/003](docs/decisions/003-homer-render-only.md)).

## Why this exists

Homepage and Glance already ship a built-in runtime Docker-label discovery,
and for most people that's enough. `dashsync` solves a different problem:
producing a **static config committed to git** — one you can review, roll
back, and deploy without giving the dashboard itself access to the Docker
socket.

The tool is built around three properties that follow from that:

- **Idempotent.** Two runs in a row with no changes in Docker produce a zero
  diff. The output is meant to be committed to git without noise.
- **Managed markers.** Generated entries are marked in the file, so a
  re-run only touches its own section: hand-written entries and comments
  are left alone, and services that disappeared get removed.
- **Multi-format.** One source of truth renders into several dashboard
  formats.

## What it doesn't do

- No web UI, no daemon with a database, no Kubernetes.
- No Homarr support — its config lives in a database, so there's nothing to
  generate.
- **Never writes to Docker.** All access to the Docker API is strictly
  read-only.

## Install

Download a prebuilt binary from the
[latest release](https://github.com/NikitaMikhailov/dashsync/releases/latest)
(linux/darwin, amd64/arm64):

```bash
os=$(uname -s)
arch=$(uname -m); case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
curl -fsSL "https://github.com/NikitaMikhailov/dashsync/releases/latest/download/dashsync_${os}_${arch}.tar.gz" \
  | tar -xz dashsync
sudo mv dashsync /usr/local/bin/
```

Every release also publishes a `checksums.txt` — download it from the same
[releases page](https://github.com/NikitaMikhailov/dashsync/releases/latest)
and check the archive against it (`sha256sum -c`) if you want to verify the
download.

Or build it yourself:

```bash
go install github.com/NikitaMikhailov/dashsync/cmd/dashsync@latest
```

(`dashsync version` on a `go install`-built binary shows the correct version
tag, but `commit`/`date` come back `unknown` — Go only embeds those from a
local git checkout's own VCS info, which a module-proxy install doesn't
have. A release binary gets both, via ldflags.)

## Usage

```bash
# List what dashsync would discover — no file written.
dashsync inspect

# Render Homepage's services.yaml to stdout.
dashsync sync

# Idempotently merge into an existing file: adds, updates, and removes
# managed entries; anything you wrote by hand is left alone.
dashsync sync --output-path services.yaml

# A second dashboard format.
dashsync sync --format homer
```

A container opts in with `dashsync.enable=true`; see
[docs/decisions/001](docs/decisions/001-label-schema.md) for the full label
contract. For discovery across more than one Docker host (TCP, TLS, a
non-default socket), point `--config` at a `dashsync.yaml` describing them
— see [docs/decisions/004](docs/decisions/004-multi-host-config-schema.md)
for its schema; every subcommand's `--help` has the full flag list.

## Development

```bash
go build ./cmd/dashsync
go test ./... -race
golangci-lint run
```

## License

[Apache-2.0](LICENSE). Chosen for its explicit patent grant: this is an
infrastructure tool that could end up inside a corporate environment, and
that's usually the first question asked there.
