# dashsync

A CLI tool that turns running Docker containers into static config files for
self-hosted dashboards (Homepage, Dashy, Homer), syncs them idempotently, and
doesn't clobber whatever you edited by hand.

> **Status: early development (M0).** No working functionality yet — the
> repository currently holds only scaffolding, linting, and CI.

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

Not yet — the first release lands at milestone M5.

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
