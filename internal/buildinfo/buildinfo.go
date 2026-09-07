// Package buildinfo provides version metadata for the running dashsync
// binary: version number, commit, and build date.
//
// TODO(author): implement resolve(). The signatures and contract are below;
// behavior is specified by the test table in buildinfo_test.go — run
// `go test ./internal/buildinfo/...` until every case is green.
package buildinfo

import "runtime/debug"

// The three variables below are the one sanctioned exception to the "no
// global state" rule in CLAUDE.md. The tool that patches them — the
// -ldflags linker flag — can only assign to package-level variables: it has
// no access to struct fields or function parameters. A release build's
// linker invocation (via GoReleaser) looks roughly like:
//
//	go build -ldflags "-X .../internal/buildinfo.version=v0.3.0 \
//	                    -X .../internal/buildinfo.commit=abc1234 \
//	                    -X .../internal/buildinfo.date=2026-09-07T12:00:00Z"
//
// On a plain build (`go build` with no ldflags) all three stay as declared
// below, and Get() then has to recover what it can from
// runtime/debug.ReadBuildInfo().
var (
	//nolint:gochecknoglobals // set via -ldflags, see the comment above
	version = "dev"
	//nolint:gochecknoglobals // set via -ldflags, see the comment above
	commit = ""
	//nolint:gochecknoglobals // set via -ldflags, see the comment above
	date = ""
)

// Info holds the build metadata for one dashsync binary. The json tags
// matter to `dashsync version --output json` in internal/cli — without
// them the output keys would be "Version"/"Commit"/"Date" instead of the
// lowercase names a CLI's JSON output is expected to use.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Get returns the build metadata for the current binary.
//
// Get's only job is to go fetch debug.ReadBuildInfo() from the runtime and
// hand everything it got to resolve(). The actual "what to show" logic
// lives in resolve(), which no runtime call reaches — I/O stays at the
// edges, logic stays pure, same principle as the rest of this codebase,
// just scoped to a single package. That's exactly why resolve() is
// testable without a single mock.
func Get() Info {
	bi, ok := debug.ReadBuildInfo()
	return resolve(version, commit, date, bi, ok)
}

// resolve takes the already-read ldflags values and the result of
// debug.ReadBuildInfo() and decides what to show the user. Contract:
//
//  1. If ldVersion != "dev" (it was set via -ldflags on a release build),
//     version, commit, and date are taken from ldflags as-is; debug.BuildInfo
//     is not consulted at all.
//  2. Otherwise, if bi.Main.Version is a real semver (e.g. "v1.4.0" — this
//     happens with `go install github.com/.../dashsync@v1.4.0`), it becomes
//     Version. debug.Module is a struct with a Version field — this is
//     exactly that case.
//  3. Otherwise Version stays "dev": a local `go build`/`go run` doesn't
//     know a version number, and there's no point making one up.
//  4. Commit and Date, if not set via ldflags, are looked up among
//     bi.Settings — a slice of debug.BuildSetting{Key, Value}. Since Go
//     1.18, the compiler stores "vcs.revision" (the full git hash) and
//     "vcs.time" there when building inside a git repository, with no
//     ldflags involved. Commit is the first 7 characters of "vcs.revision"
//     (a short hash, like `git rev-parse --short`), with a "+dirty" suffix
//     when "vcs.modified" is "true".
//  5. If ok is false or Settings carries none of these keys, the
//     corresponding Info field stays the empty string (for Version, that
//     means "dev", per point 3). This is the case where "don't panic,
//     handle it" separates an infrastructure tool from a learning script: a
//     binary can be built outside a VCS checkout (say, unpacked from a
//     tarball) — that's not an error condition.
func resolve(ldVersion, ldCommit, ldDate string, bi *debug.BuildInfo, ok bool) Info {
	panic("TODO: implement per the contract above and the tests in buildinfo_test.go")
}
