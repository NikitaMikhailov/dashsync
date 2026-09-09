# 010 — Docker image for dashsync itself

## Context

dashsync's only distribution channel so far is a raw binary (a `.tar.gz`
per platform on the GitHub release, or `go install`). That covers running
it directly on a host, but dashsync's actual audience — people already
running Homepage/Homer/Dashy and the containers it discovers — very
plausibly wants to run dashsync itself as a scheduled container too (a
cron-in-a-container sidecar, an `ofelia` job, a Kubernetes CronJob), the
same way they already run everything else. Considered alongside two other
packaging options (`.deb`/`.rpm`, a Homebrew tap) and picked as the one
worth doing now — see "Alternatives considered" for why the other two
were passed on.

## Decision

A multi-arch (`linux/amd64`, `linux/arm64`) image, built by GoReleaser and
pushed to `ghcr.io/nikitamikhailov/dashsync` on every tagged release,
tagged both `{{ .Version }}` (immutable, for pinning) and `latest`
(mutable, for `docker pull` without looking up a version first — the one
deliberate exception to the versionless-only naming the `.goreleaser.yaml`
archives section otherwise avoids, for the opposite reason: a filename
needs to be stable across versions for `/releases/latest/download/` to
work, a tag is *supposed* to move).

**`dockers_v2`, not the older `dockers`/`docker_manifests`.** The exact
GoReleaser version this project pins (2.18.1, per ADR 006) already
deprecates the classic two-block approach (one entry per architecture,
manually stitched into a manifest list) in favor of `dockers_v2`, which
builds a real multi-arch manifest in one entry via `docker buildx build
--push` in a single run. Starting on the deprecated path when the
replacement is already stable in the exact pinned version would just be
debt owed on day one.

**`ghcr.io`, not Docker Hub.** No new registry account, no new secret:
GoReleaser's `dockers_v2` step authenticates with whatever `docker login`
session already exists in the job, and `release.yml`'s existing
`GITHUB_TOKEN` (with `packages: write` added to the workflow's
permissions) is enough to push to a package under this same GitHub
account — one `docker/login-action` step against `ghcr.io` using that
token, nothing GoReleaser-specific to configure for auth at all.

**`alpine:3.20` base, with `openssh-client` installed.** Not `scratch` or
a distroless image: dashsync's `ssh://` support (ADR 009) works by
exec'ing the system `ssh` binary, and a base image with no shell and no
`ssh` would silently make that entire discovery scheme non-functional
inside the container — only discoverable at connection time, not at
image-build time, which is a worse place to find out. `ca-certificates`
is the other package installed, for the same reason any HTTPS/TLS client
needs a real root store — `tcp://`+TLS hosts (ADR 004) depend on it.
Alpine keeps the image small (image size still dominated by the ~10-30MB
static Go binary, not the base) while keeping every address scheme this
project supports actually usable inside the container, not just the ones
that happen not to need anything extra.

**`$TARGETPLATFORM` in the Dockerfile, not a flat binary path.** A single
`dockers_v2` entry builds every platform in one buildx invocation, laying
each platform's binary into the build context under
`$TARGETPLATFORM/dashsync` (buildx sets that build arg automatically per
platform) rather than a flat `dashsync` at the context root — the
Dockerfile's `COPY $TARGETPLATFORM/dashsync ...` line is required, not
stylistic, for multi-arch to resolve to the right binary per platform.

## A real operational note found while testing this, not just assumed

Running the image against a `unix://` Docker socket is exactly what it
looks like — `-v /var/run/docker.sock:/var/run/docker.sock`, nothing
else. Running it against an `ssh://` host is not quite as simple as on a
bare host, and this was confirmed by actually doing it, not reasoned
about: the container has no `~/.ssh/known_hosts` of its own and no
identity unless given one. The working pattern, verified against a real
SSH-reachable host:

```bash
docker run --rm \
  -v /path/to/dashsync.yaml:/dashsync.yaml:ro \
  -v ~/.ssh/known_hosts:/root/.ssh/known_hosts:ro \
  -v $SSH_AUTH_SOCK:$SSH_AUTH_SOCK \
  -e SSH_AUTH_SOCK \
  ghcr.io/nikitamikhailov/dashsync inspect --config /dashsync.yaml
```

Forwarding the agent socket alone isn't enough — without a mounted
`known_hosts`, `BatchMode=yes` (ADR 009) fails a first connection from
inside the container exactly the way it would on a fresh host that's
never run `ssh` to that address before, since the container *is*
approximately that: a filesystem with no history with the remote host at
all. Worth a line in the README, not just here.

## Alternatives considered

- **`.deb`/`.rpm` packages** (via GoReleaser's `nfpms` integration).
  Passed on for now: without an actual apt/yum repository — real,
  separately-hosted infrastructure with its own signing and maintenance
  burden, not something GoReleaser produces on its own — a bare package
  file attached to a GitHub release doesn't meaningfully beat the
  existing `curl | tar` install snippet. It would be another
  release-pipeline surface (package metadata, `nfpm` config) maintained
  for marginal benefit over what already works, with no signal anyone's
  asked for it specifically — the same "don't build for a hypothetical"
  reasoning this project applies elsewhere.
- **A Homebrew tap.** Genuinely more valuable than a bare `.deb` — `brew
  upgrade` is a real update mechanism a downloaded package file doesn't
  have, and this project's plausible audience skews toward exactly the
  platform Homebrew serves. Not done in this change because it needs the
  user's own one-time setup outside anything this repo controls: a
  separate `homebrew-tap` repository and a personal access token stored
  as a secret here, neither of which a coding change can create on its
  own. Left for a follow-up once that setup exists.

## A second gap only a real CI runner would have caught

Multi-platform `dockers_v2` needs a buildx builder that actually supports
multi-platform output, plus QEMU to emulate the non-native architecture.
Docker Desktop provides both automatically with zero configuration —
which is exactly why the first version of this change worked completely
untouched in local testing, and would *not* have worked the first time a
real tag reached a bare GitHub Actions runner, which ships buildx but
neither a multi-platform-capable builder nor QEMU pre-installed. Both
`release.yml` and `ci.yml`'s docker check job now run
`docker/setup-qemu-action` and `docker/setup-buildx-action` before
touching `dockers_v2` — GoReleaser's own documented prerequisite for this
feature on Linux. Found in review, specifically because "verified by
running it" had only been checked against Docker Desktop, not the actual
CI environment the release genuinely runs in — a reminder that "I ran it
and it worked" still needs to ask *where*.

## Consequences

- `release.yml` gained `docker/setup-qemu-action`, `docker/setup-buildx-action`,
  and `docker/login-action` steps and a `packages: write` permission —
  the release job can now publish GitHub Packages under this
  repository's account, a capability it didn't have before.
- `ci.yml` gained a new `docker-build-check` job (not folded into the
  existing `lint` job — a full snapshot build is a heavier, differently-scoped
  check than golangci-lint/`go mod tidy -diff`/`goreleaser check`, and
  keeping it separate means a red "lint" status still means what it's
  always meant). `goreleaser check` alone only validates schema — it
  never actually builds the Dockerfile, so a broken buildx setup or a
  `$TARGETPLATFORM` typo would otherwise have no signal before a real tag
  push failed inside `release.yml`.
- The image is unauthenticated and public once pushed, same as the
  existing GitHub Releases binaries — dashsync ships no secrets and reads
  no private state, so this doesn't change what's being trusted, just
  the format it's trusted in.
- `latest` is a mutable tag pointing at whichever release ran most
  recently, not necessarily the highest semver — the standard risk with
  any mutable "latest," not specific to this image: a hotfix release cut
  for an older minor after a newer one has already shipped would move
  `latest` backward. Unlikely given this project's release history so
  far, not specially guarded against.
- The base image is pinned by digest (`alpine:3.20@sha256:...`), not just
  the tag, matching this project's pinning discipline everywhere else —
  `.github/dependabot.yml` gained a `docker` ecosystem entry so that
  digest still gets bumped automatically, readably, instead of silently
  drifting with whatever `alpine:3.20` happens to resolve to on a given
  build day.
