# 011 — Homebrew tap

## Context

Considered alongside the Docker image and passed on at the time (see
[ADR 010](010-docker-image.md)'s "Alternatives considered"): unlike a
bare `.deb`/`.rpm`, `brew upgrade` is a real update mechanism, and this
project's plausible audience skews toward exactly the platform Homebrew
serves. What blocked it then wasn't the GoReleaser config — it's that
publishing a tap needs infrastructure a coding change can't create on
its own: a separate `NikitaMikhailov/homebrew-tap` repository, and a
credential with write access to it. Both now exist (repo created via
`gh repo create`; a fine-grained PAT scoped to only that repo's
contents, stored as this repo's own `HOMEBREW_TAP_GITHUB_TOKEN` secret —
neither the PAT itself nor adding it as a secret could be done by an
agent: GitHub has no API for creating a personal access token, precisely
because that's meant to require the account holder's own session).

## Decision

`brews:` in `.goreleaser.yaml` pushes a formula to
`NikitaMikhailov/homebrew-tap` on every release, so
`brew install nikitamikhailov/tap/dashsync` works after one `brew tap
nikitamikhailov/tap`. `release.yml` passes `HOMEBREW_TAP_GITHUB_TOKEN`
through as an env var the `token:` template reads — a separate
credential from `GITHUB_TOKEN` on purpose: `GITHUB_TOKEN` is scoped to
*this* repository only, by GitHub Actions itself, regardless of anything
in this repo's own `permissions:` block, and pushing a commit to a
different repository needs a credential that actually has write access
there.

## `brews`, not `homebrew_casks` — deliberately keeping a deprecated field

Running `goreleaser check` against a config using `brews` fails outright:
`DEPRECATED: brews should not be used anymore`, treated as a hard error,
not a warning — confirmed directly, not assumed. GoReleaser's own
deprecation guidance directs users to `homebrew_casks` instead. Checked
before switching, also directly, not assumed: **Homebrew Casks are
macOS-only** — there is no Linux/Linuxbrew support for casks at all, only
for the older Formula (`brews`) mechanism. dashsync ships `linux/amd64`
and `linux/arm64` binaries as a first-class target (see `.goreleaser.yaml`'s
`builds:` section) to an audience of self-hosters who very plausibly run
Homebrew on Linux, not just macOS. Switching to the "supported"
replacement would silently drop Linux Homebrew users entirely to satisfy
a linter-shaped concern — the wrong trade, and the opposite of this
project's own "verify empirically before assuming a replacement is
actually equivalent" discipline (see ADR 010's `dockers`/`dockers_v2`
migration, where the replacement *was* a full equivalent — this is the
case where it isn't).

**Consequence: `goreleaser check` was removed from `ci.yml` entirely**,
not worked around. `check` has no flag to allow a deliberately-kept
deprecated field, and `brews` triggers its failure unconditionally,
whether or not anything is actually wrong. Confirmed side by side: `goreleaser
release --snapshot --clean --skip=publish` (already running in `ci.yml`'s
`docker-build-check` job, added in ADR 010) parses and exercises the
exact same config, prints the identical deprecation notice, but only as
a warning — the run still succeeds. That job already validates this
config more thoroughly than `check` ever did (it actually builds every
target, not just checks schema shape), so removing `check` loses no real
coverage — it loses a subcommand that, as of this exact dependency, can
no longer tell "deprecated on purpose" apart from "actually broken."

## What's genuinely unverified, unlike everything else in this change

Every other claim above was checked directly, not assumed — but the
actual push to `homebrew-tap` is the one step that can't be: it needs
the real `HOMEBREW_TAP_GITHUB_TOKEN`, which only exists as a GitHub
Actions secret, and `--skip=publish` (used for every local/CI dry run so
far) deliberately never exercises it. `homebrew-tap` is a real, public,
currently-empty repository — nothing has ever pushed a first commit into
it yet. The first real tag push is where this either works or fails,
with no CI signal beforehand — worth watching closely rather than
assuming success, the same way the Docker image's QEMU/buildx gap (ADR
010) only surfaced on a real runner, not in local testing.

## Consequences

- A `brew install`/`brew upgrade` path now exists for macOS and Linux
  alike, the gap a `.deb` alone couldn't close (see ADR 010).
- `ci.yml` no longer has a schema-only GoReleaser validation step —
  `docker-build-check`'s full snapshot build is what catches a config
  regression now, on every PR, same as before, just via a different
  command.
- If GoReleaser ever removes `brews` outright (past deprecation into
  actual deletion) before a Linux-compatible Cask alternative exists,
  dashsync loses this distribution channel at that point, not before —
  worth revisiting this decision if that happens, not before.
- The formula's `description` is copied from `internal/cli/root.go`'s
  own `Short` field verbatim, on purpose — one place describing what
  this tool is, not two that can quietly drift apart.
