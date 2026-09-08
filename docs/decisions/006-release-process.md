# 006 — Release process

## Context

`internal/buildinfo` (M0) was built anticipating this from the start: its
package comment documents the exact `-ldflags` invocation a release build
is expected to make, and `resolve()`'s "ldflags win outright on a release
build" logic has been sitting untested-in-anger since M0. M5 is where that
contract actually gets exercised, and where dashsync gets something
installable for the first time — the README's "Install" section has said
"not yet — the first release lands at milestone M5" since M0.

## Decisions

**GoReleaser, not a hand-rolled build matrix script.** The CI `build` job
already cross-compiles for linux/darwin × amd64/arm64 to catch a
compilation failure early, but it throws the binaries away
(`go build -o /dev/null`) — it was never meant to produce release
artifacts. GoReleaser owns archiving, checksums, changelog generation, and
GitHub Release creation as one well-tested tool instead of reimplementing
each piece by hand. It also directly consumes the `-ldflags` contract
`internal/buildinfo` already documents, so wiring it up was one config
file, not a design decision about how to inject version info.

**Same platform matrix as CI, no more.** linux/darwin × amd64/arm64 — the
`.goreleaser.yaml` `builds` section is deliberately not more ambitious than
`.github/workflows/ci.yml`'s own matrix. Windows and 386 (both in
GoReleaser's own `init`-generated template) are left out: dashsync has
never built for either, has no test coverage under either, and "the
release process supports more platforms than CI does" would be a release
artifact nobody has actually verified works.

**Versionless archive filenames.** `dashsync_Darwin_arm64.tar.gz`, not
`dashsync_v0.3.0_Darwin_arm64.tar.gz`. GitHub's
`/releases/latest/download/<filename>` URL always resolves to whatever
release is newest, but only if `<filename>` is identical across releases —
that's what lets the README's install snippet hardcode one URL instead of
looking up a version number first. Per-download provenance still comes
from `checksums.txt` (attached to every release, filename also fixed) and
the release page itself.

**Archive names use Go's own arch terms (`amd64`/`arm64`), not `uname
-m`'s.** `uname -m` reports `x86_64` for amd64 everywhere, but reports
`aarch64` for arm64 on Linux while reporting `arm64` on macOS — the same
architecture, two different strings, and the difference is OS-specific,
not something a single substitution fixes. Baking `uname -m`'s output into
the archive name would mean either matching `aarch64` on Linux (and
`arm64` on Darwin) with two different naming rules per OS, or accepting
that the install snippet can't just interpolate raw `uname` output.
Simpler: keep the archive name in Go's own vocabulary, and have the
install snippet translate `uname -m`'s two spellings of amd64/arm64 to
GOARCH once, in one place, in shell.

**Changelog grouped and filtered by Conventional Commit type.** `feat`/
`fix` get their own headed section; `docs`/`chore`/`test`/`ci` commits (and
merge commits) are excluded entirely. A GitHub Release's changelog is
read by someone deciding whether to upgrade, not by someone auditing every
commit — CLAUDE.md's own Conventional Commits convention already sorts
commits into "user-visible change" versus "not," so the changelog just
uses that sort instead of inventing a second one.

## Alternatives considered

- **A Homebrew tap, a Docker image, or Linux packages (deb/rpm) in the
  same milestone.** Deferred, not rejected: GoReleaser supports all three
  with a few more config blocks, but each adds its own maintenance surface
  (a tap repo, a registry, package signing) with no evidence yet that
  anyone wants dashsync installed that way. `go install` and a raw binary
  download cover the realistic first-release audience; revisit if a real
  request shows up.
- **Baking `uname -m`'s architecture spelling directly into the archive
  name** (so the install snippet needs no translation at all). Rejected
  once Linux's `aarch64` vs. Darwin's `arm64` for the identical
  architecture was actually checked rather than assumed — "no translation
  needed" turned into "two different, OS-conditional naming rules," which
  is more to get wrong than one shell `case` statement in the one place
  that already has to know about both spellings.

## Consequences

- Cutting a release is `git tag vX.Y.Z && git push --tags` — the tag push
  triggers `.github/workflows/release.yml`, which runs
  `goreleaser release --clean` and does everything else.
- `internal/buildinfo`'s `resolve()` logic (ldflags win on a release build,
  `runtime/debug.ReadBuildInfo()` otherwise) is exercised for the first
  time by a real release build here, via a local
  `goreleaser release --snapshot --clean --skip=publish` verified before
  this shipped — not just asserted true by the M0-era table tests, which
  necessarily could only test `resolve()`'s pure logic in isolation, never
  a real `-ldflags`-built binary's `dashsync version` output.
- No Docker image or package-manager install path exists yet. Anyone
  wanting one has exactly the two options in the README: download a
  binary, or `go install`.
- The very first tag's changelog covers every `feat`/`fix` commit since
  the repository began (M1 through M4), not "since the last release" —
  there is no last release yet. Expected and harmless for a first
  changelog, but worth knowing going in: it'll read as a much longer list
  than every subsequent release's changelog will.
- `dashsync version`'s `date` field is the tagged commit's own timestamp,
  not when the binary was compiled — see `internal/buildinfo/buildinfo.go`'s
  package comment for why, and why the CLI labels it `date:` rather than
  `built:`.
