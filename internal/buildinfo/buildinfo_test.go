package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		commit  string
		date    string
		bi      *debug.BuildInfo
		ok      bool
		want    Info
	}{
		{
			name:    "ldflags override wins over VCS info",
			version: "v0.3.0",
			commit:  "abc1234",
			date:    "2026-09-07T12:00:00Z",
			bi: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "cafefeedcafefeedcafefeedcafefeedcafefeed"},
					{Key: "vcs.time", Value: "2026-01-01T00:00:00Z"},
				},
			},
			ok:   true,
			want: Info{Version: "v0.3.0", Commit: "abc1234", Date: "2026-09-07T12:00:00Z"},
		},
		{
			name:    "plain go build inside a clean git checkout",
			version: "dev",
			bi: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"},
					{Key: "vcs.time", Value: "2026-09-07T10:00:00Z"},
					{Key: "vcs.modified", Value: "false"},
				},
			},
			ok:   true,
			want: Info{Version: "dev", Commit: "deadbee", Date: "2026-09-07T10:00:00Z"},
		},
		{
			name:    "dirty working tree is marked",
			version: "dev",
			bi: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"},
					{Key: "vcs.time", Value: "2026-09-07T10:00:00Z"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			ok:   true,
			want: Info{Version: "dev", Commit: "deadbee+dirty", Date: "2026-09-07T10:00:00Z"},
		},
		{
			name:    "go install of a tagged module version, no ldflags",
			version: "dev",
			bi: &debug.BuildInfo{
				Main: debug.Module{Version: "v1.4.0"},
			},
			ok:   true,
			want: Info{Version: "v1.4.0"},
		},
		{
			// Since Go 1.24-ish, a plain `go build` inside a dirty VCS
			// checkout synthesizes a pseudo-version for the main module
			// instead of "(devel)" — this must still resolve to "dev", not
			// be shown as if it were a real release.
			name:    "plain go build synthesizes a pseudo-version, not a release",
			version: "dev",
			bi: &debug.BuildInfo{
				Main: debug.Module{Version: "v0.0.0-20260907172338-407506951914+dirty"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "40750695191440d37b4d6e46fb2e6a022669d469"},
					{Key: "vcs.time", Value: "2026-09-07T17:23:38Z"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			ok:   true,
			want: Info{Version: "dev", Commit: "4075069+dirty", Date: "2026-09-07T17:23:38Z"},
		},
		{
			name:    "no build info available at all",
			version: "dev",
			bi:      nil,
			ok:      false,
			want:    Info{Version: "dev"},
		},
		{
			name:    "build info present but carries no VCS settings",
			version: "dev",
			bi: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
			},
			ok:   true,
			want: Info{Version: "dev"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := resolve(tt.version, tt.commit, tt.date, tt.bi, tt.ok)
			if got != tt.want {
				t.Errorf("resolve(%q, %q, %q, ..., ok=%v) = %+v, want %+v",
					tt.version, tt.commit, tt.date, tt.ok, got, tt.want)
			}
		})
	}
}
