# dashsync

A CLI tool that turns running Docker containers into static config files for
self-hosted dashboards — [Homepage](https://gethomepage.dev),
[Homer](https://github.com/bastienwirtz/homer), and
[Dashy](https://dashy.to) — syncs them idempotently, and doesn't clobber
whatever you edited by hand.

> **Status: pre-1.0, functional.** Docker label discovery works, single- or
> multi-host. All three formats — Homepage, Homer, and Dashy — get the full
> idempotent merge (see
> [docs/decisions/007](docs/decisions/007-document-adapter.md)).

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

Via Homebrew (macOS or Linux):

```bash
brew install nikitamikhailov/tap/dashsync
```

Or download a prebuilt binary from the
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

Or run it as a container — a multi-arch (amd64/arm64) image is published
to `ghcr.io/nikitamikhailov/dashsync` on every release:

```bash
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/nikitamikhailov/dashsync inspect
```

An `ssh://` host (see [docs/decisions/009](docs/decisions/009-ssh-docker-discovery.md))
needs a bit more mounted in, since the container starts with no SSH
history of its own — a `known_hosts` and a forwarded agent socket, not
just the config file:

```bash
docker run --rm \
  -v /path/to/dashsync.yaml:/dashsync.yaml:ro \
  -v ~/.ssh/known_hosts:/root/.ssh/known_hosts:ro \
  -v $SSH_AUTH_SOCK:$SSH_AUTH_SOCK -e SSH_AUTH_SOCK \
  ghcr.io/nikitamikhailov/dashsync inspect --config /dashsync.yaml
```

See [docs/decisions/010](docs/decisions/010-docker-image.md) for why the
image is built the way it is.

## Usage

```bash
# List what dashsync would discover — no file written.
dashsync inspect

# Render Homepage's services.yaml to stdout.
dashsync sync

# Idempotently merge into an existing file: adds, updates, and removes
# managed entries; anything you wrote by hand is left alone.
dashsync sync --output-path services.yaml

# Homer and Dashy support the same idempotent merge, into their own
# formats (config.yml and conf.yml are each format's own conventional
# filename — --output-path accepts any path).
dashsync sync --format homer --output-path config.yml
dashsync sync --format dashy --output-path conf.yml
```

A container opts in with `dashsync.enable=true`; see
[docs/decisions/001](docs/decisions/001-label-schema.md) for the full label
contract. For discovery across more than one Docker host (TCP, TLS, a
non-default socket, or SSH — `ssh://user@host[:port]`, no separate
identity/agent config: it reuses your own `ssh` and `~/.ssh/config`
exactly like `docker -H ssh://...` does, right down to needing a manual,
interactive `ssh` run once to accept a host's key before dashsync's own
non-interactive connection can reach it), point `--config` at a
`dashsync.yaml` describing them — see
[docs/decisions/004](docs/decisions/004-multi-host-config-schema.md) for
its schema and [docs/decisions/009](docs/decisions/009-ssh-docker-discovery.md)
for how the SSH case works; every subcommand's `--help` has the full flag
list.

## Typical workflow

`dashsync` has no git integration of its own — it just writes a plain
file, and that file happens to live in a directory you already keep in
git. That's the entire mechanism:

```bash
# Added a container, want the dashboard to catch up.
$ dashsync sync --output-path services.yaml
+ Media: Jellyfin (added)
1 change(s): 1 added, 0 updated, 0 removed, 0 conflict(s)

$ git diff services.yaml
+    # dashsync:managed id=4a0a65f45558 content=6ac06496
+    - Jellyfin:
+        href: http://10.0.0.5:8096

$ git add services.yaml && git commit -m "dashboard: add Jellyfin" && git push
```

From there it's an ordinary commit, reviewed and rolled back the same way
as any other config change — `dashsync` doesn't know or care that git is
involved. Two common ways to trigger the `sync` step itself:

- **By hand**, right after adding or changing a container — enough for a
  small setup.
- **On a schedule** (cron, a systemd timer), once "did anyone remember to
  run sync" becomes a real question. A run that finds no changes exits
  quietly; one that finds changes can auto-commit (fine for a low-stakes
  personal setup) or open a pull request for a human to review before it
  ships — worth it once a service silently disappearing from the
  dashboard is something you'd want to catch before it goes live, not
  after. `--dry-run` defaults to true whenever `$CI` is set, so a
  scheduled job meant to actually write needs `--dry-run=false` — an
  automation shell that happens to export `$CI` for unrelated reasons
  will otherwise print a change summary and write nothing.

A separate, scheduled job — one that shouldn't write anything, just catch
a committed file going stale relative to what's actually running — wants
`--check` instead of the auto-commit/open-a-PR pattern above: it never
writes, and exits with a distinct code (2, not the generic 1 every other
failure gets) when the file has pending changes, so "the check found
drift" can be told apart from "the check itself broke." Since discovery
only ever reads Docker's *current* state (dashsync never inspects a
compose file or a PR diff — see "Why this exists" above), this job needs
to run somewhere that can already see the container in question, which
makes it a post-deploy drift check, not a pre-merge PR gate: a container
added in a PR isn't running anywhere yet for `--check` to discover until
after that PR merges and deploys.

```bash
dashsync sync --output-path services.yaml --check
```

Either way, the dashboard itself only ever reads the rendered file off
disk — it never talks to Docker and doesn't care whether `dashsync` or a
human wrote what it's looking at.

`sync --output-path` also leaves a `<output-path>.lock` file next to it,
permanently — an empty sidecar used to keep two overlapping runs from
clobbering each other (see [ADR 008](docs/decisions/008-cross-process-locking.md)).
It carries no content worth committing; add it to your own `.gitignore`.

This locking is best-effort, not a guarantee: it relies on the output
path living on an ordinary filesystem. A directory bind-mounted into a
container through certain virtualized filesystem layers (confirmed
against Docker Desktop's virtiofs specifically) can let two writers each
believe they hold the lock at once, silently. If `dashsync` itself runs
inside a container writing to a bind-mounted config directory, keep
overlapping schedules serialized yourself rather than relying on this —
see ADR 008 for why there's no portable fix available at this layer.

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
