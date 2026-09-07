package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/NikitaMikhailov/dashsync/internal/buildinfo"
)

func TestVersionCmd_TextOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info buildinfo.Info
		want string
	}{
		{
			name: "full metadata",
			info: buildinfo.Info{Version: "v0.3.0", Commit: "abc1234", Date: "2026-09-07T12:00:00Z"},
			want: "dashsync v0.3.0\ncommit:  abc1234\nbuilt:   2026-09-07T12:00:00Z\n",
		},
		{
			name: "dev build without VCS info",
			info: buildinfo.Info{Version: "dev"},
			want: "dashsync dev\ncommit:  unknown\nbuilt:   unknown\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := newVersionCmd(func() buildinfo.Info { return tt.info })
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetArgs(nil)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() = %v, want nil", err)
			}
			if got := stdout.String(); got != tt.want {
				t.Errorf("stdout = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionCmd_JSONOutput(t *testing.T) {
	t.Parallel()

	info := buildinfo.Info{Version: "v0.3.0", Commit: "abc1234", Date: "2026-09-07T12:00:00Z"}
	cmd := newVersionCmd(func() buildinfo.Info { return info })

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--output", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	var got buildinfo.Info
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output did not parse as JSON: %v\noutput: %s", err, stdout.String())
	}
	if got != info {
		t.Errorf("json output = %+v, want %+v", got, info)
	}
}

func TestVersionCmd_UnknownOutputFlag(t *testing.T) {
	t.Parallel()

	cmd := newVersionCmd(func() buildinfo.Info { return buildinfo.Info{Version: "dev"} })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output", "xml"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() = nil, want an error for an unsupported --output value")
	}
	if !strings.Contains(err.Error(), "xml") {
		t.Errorf("error = %q, want it to mention the passed value %q", err.Error(), "xml")
	}
}
