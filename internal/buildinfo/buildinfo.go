// Package buildinfo provides version metadata for the running dashsync
// binary: version number, commit, and build date.
package buildinfo

import (
	"regexp"
	"runtime/debug"
)

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

// Get returns the build metadata for the current binary. The runtime call
// (debug.ReadBuildInfo) and the decision logic (resolve) are split so the
// latter is testable with a table and no mocks.
func Get() Info {
	bi, ok := debug.ReadBuildInfo()
	return resolve(version, commit, date, bi, ok)
}

// pseudoVersionRE matches a Go module pseudo-version's distinctive tail: a
// 14-digit timestamp and commit hash (see go.dev/ref/mod#pseudo-versions).
// The toolchain synthesizes one of these for the main module's own version
// on a plain `go build` inside a VCS checkout — it's not a release, so it
// shouldn't be presented as one.
var pseudoVersionRE = regexp.MustCompile(`-\d{14}-[0-9a-fA-F]+(\+(incompatible|dirty))?$`)

// resolve decides what `dashsync version` should show, given the ldflags
// values and the result of debug.ReadBuildInfo(). ldflags win outright on a
// release build; otherwise it falls back to whatever the runtime knows.
func resolve(ldVersion, ldCommit, ldDate string, bi *debug.BuildInfo, ok bool) Info {
	if ldVersion != "dev" {
		return Info{Version: ldVersion, Commit: ldCommit, Date: ldDate}
	}

	info := Info{Version: "dev"}
	if !ok {
		return info
	}

	if isReleaseVersion(bi.Main.Version) {
		info.Version = bi.Main.Version
	}

	var revision string
	var dirty bool
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.time":
			info.Date = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}

	if revision != "" {
		commit := revision[:min(7, len(revision))]
		if dirty {
			commit += "+dirty"
		}
		info.Commit = commit
	}

	return info
}

// isReleaseVersion reports whether v identifies a real, tagged module
// version rather than one of the placeholders the toolchain uses for an
// untagged build: "(devel)" (older Go) or a synthesized pseudo-version
// (current Go, when building inside a VCS checkout).
func isReleaseVersion(v string) bool {
	return v != "" && v != "(devel)" && !pseudoVersionRE.MatchString(v)
}
