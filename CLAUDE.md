# dashsync

A Go CLI that generates and idempotently syncs config files for self-hosted
dashboards (Homepage, Homer, Dashy) from the Docker API.

## Scope

- Read-only against Docker. dashsync never writes to Docker, never deploys
  or restarts containers.
- No web UI, no daemon, no database, no Kubernetes support.
- No Homarr support — its config lives in a database, there's nothing to
  generate.
- Complements Homepage/Glance's built-in label discovery; it doesn't
  replace it. The value here is a deterministic, git-committed config file.

## Core packages

`internal/merge`, `internal/model`, `internal/render/*`, `internal/cli`,
`internal/discovery`, `internal/config`, `internal/diff`, and
`internal/buildinfo` hold this project's real logic. Implement them
fully — production quality, tested, no stubs or TODOs left for later.
Keep doc comments normal and concise; a deeper walkthrough of a specific
piece happens on request, not as running commentary on every commit.

## Code rules

- Only the standard library and already-approved dependencies (see
  `go.mod`). Adding a new one is a discussion first, code second — enforced
  by the `depguard` linter, not by convention: an import outside the
  allowlist fails CI.
- Errors: `fmt.Errorf("context: %w", err)`. No `panic` outside of `main`.
- `context.Context` is always the first parameter, never a struct field.
- Interfaces are small and declared by the consumer, not next to the
  implementation.
- Renderer output is deterministic: explicit sorting, never `range` over a
  map when order matters.
- File writes are atomic: write to a temp file, then rename.
- No package-level mutable state (`gochecknoglobals`). The one sanctioned
  exception is the `-ldflags`-injected version variables in
  `internal/buildinfo`, each marked with a `//nolint` explaining why.

## Commands

- `go test ./... -race` — tests
- `go test ./... -update` — refresh golden files
- `go test -tags=integration ./... -race` — end-to-end tests against a real Docker daemon (needs one reachable; skips cleanly if not)
- `golangci-lint run` — lint
- `golangci-lint config verify` — validate `.golangci.yml`
- `go build ./cmd/dashsync` — build
- `goreleaser release --snapshot --clean --skip=publish` — dry-run a release locally, no tag or publish needed
- `git tag vX.Y.Z && git push origin vX.Y.Z` — cut a real release (see `docs/decisions/006-release-process.md`)

## Process

- Anything beyond a typo-level fix starts in plan mode.
- Tests are written before the implementation.
- Small commits, Conventional Commits (`feat:`, `fix:`, `refactor:`,
  `test:`, `docs:`, `chore:`).
- One branch per change, a PR even against this same repo. `main` is
  protected: PRs only, required checks must pass.
- Run the `reviewer` subagent over the diff before committing.
- For a decision worth remembering (a dependency choice, a rejected
  alternative, a schema design), add an ADR under `docs/decisions/`.
- No AI-attribution trailers or footers in commit messages or PR
  descriptions (no `Co-Authored-By: Claude ...`, no "Generated with ..."
  line) — ever, in this repo.

## Don't

- Don't grow the scope: no web UI, no Kubernetes, no Homarr support.
- Don't add abstractions "for later."
- Don't leave stubs or TODOs in core packages — finish what you start.
- Don't praise the code. Look for what's wrong with it.
