package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/google/go-cmp/cmp"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

func TestSyncCmd_RendersHomepageByDefault(t *testing.T) {
	t.Parallel()

	services := []model.Service{
		{Name: "Jellyfin", Group: "Media", URL: "http://10.0.0.5:8096"},
	}

	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return services, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	want := "- Media:\n  - Jellyfin:\n      href: http://10.0.0.5:8096\n"
	if stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestSyncCmd_GroupsBeforeRendering(t *testing.T) {
	t.Parallel()

	// Scrambled group/name order on the way in — sync must group+sort
	// before handing off to the renderer, not just pass services through.
	services := []model.Service{
		{Name: "sonarr", Group: "Media"},
		{Name: "adguard", Group: "Network"},
		{Name: "jellyfin", Group: "Media"},
	}

	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return services, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	// Decode structurally rather than searching the raw text for
	// substrings: a service's own description or icon could just as
	// easily contain the literal text "Media" or "sonarr" and pass a
	// position-based string search for the wrong reason entirely.
	var groups []map[string][]map[string]any
	if err := yaml.Unmarshal(stdout.Bytes(), &groups); err != nil {
		t.Fatalf("output did not parse as YAML: %v\noutput:\n%s", err, stdout.String())
	}

	gotGroupNames := groupKeys(groups)
	wantGroupNames := []string{"Media", "Network"}
	if diff := cmp.Diff(wantGroupNames, gotGroupNames); diff != "" {
		t.Errorf("group order mismatch (-want +got):\n%s", diff)
	}

	mediaServices := serviceKeys(groups[0]["Media"])
	wantMediaServices := []string{"jellyfin", "sonarr"}
	if diff := cmp.Diff(wantMediaServices, mediaServices); diff != "" {
		t.Errorf("service order within Media mismatch (-want +got):\n%s", diff)
	}
}

func groupKeys(groups []map[string][]map[string]any) []string {
	names := make([]string, len(groups))
	for i, g := range groups {
		for name := range g {
			names[i] = name
		}
	}
	return names
}

func serviceKeys(services []map[string]any) []string {
	names := make([]string, len(services))
	for i, s := range services {
		for name := range s {
			names[i] = name
		}
	}
	return names
}

func TestSyncCmd_UnknownFormat(t *testing.T) {
	t.Parallel()

	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return nil, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--format", "dashy"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() = nil, want an error for an unsupported --format value")
	}
	if !strings.Contains(err.Error(), "dashy") {
		t.Errorf("error = %q, want it to mention the passed value %q", err.Error(), "dashy")
	}
}

func TestSyncCmd_DiscoveryError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connect to docker: no such host")
	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return nil, wantErr })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(nil)

	if err := cmd.Execute(); !errors.Is(err, wantErr) {
		t.Errorf("Execute() error = %v, want it to be (or wrap) %v", err, wantErr)
	}
}

func TestSyncCmd_OutputPath_WritesFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")
	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}

	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return services, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--output-path", path, "--dry-run=false"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("sync did not create %s: %v", path, err)
	}
	if !strings.Contains(string(got), "Jellyfin") {
		t.Errorf("written file = %q, want it to contain Jellyfin", got)
	}
	if !strings.Contains(stdout.String(), "added") {
		t.Errorf("stdout = %q, want a change summary mentioning \"added\"", stdout.String())
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Error("a .bak file should not be created when there was nothing to back up")
	}
}

func TestSyncCmd_OutputPath_PreservesExistingFilePermissions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")
	// 0644 is common for a config file bind-mounted into a container
	// running as some other UID that still needs to read it — 0640
	// specifically, so this test fails loudly if writeAtomic ever falls
	// back to a hardcoded default instead of actually reading the
	// original mode.
	const original = 0o640
	if err := os.WriteFile(path, []byte("- Media: []\n"), original); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}
	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return services, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output-path", path, "--dry-run=false"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != original {
		t.Errorf("file mode after sync = %o, want it unchanged at %o", got, original)
	}

	backupInfo, err := os.Stat(path + ".bak")
	if err != nil {
		t.Fatalf("stat %s.bak: %v", path, err)
	}
	if got := backupInfo.Mode().Perm(); got != original {
		t.Errorf(".bak mode = %o, want it to match the original %o too", got, original)
	}
}

func TestSyncCmd_OutputPath_NewFileGetsReadableDefaultPermissions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")
	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}

	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return services, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output-path", path, "--dry-run=false"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	// Not os.CreateTemp's 0600 default: a brand-new dashboard config
	// should be readable by more than just its owner.
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("new file mode = %o, want %o", got, 0o644)
	}
}

func TestSyncCmd_OutputPath_DryRunDoesNotWrite(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")
	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}

	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return services, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--output-path", path, "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("--dry-run must not create the output file")
	}
	if !strings.Contains(stdout.String(), "added") {
		t.Errorf("stdout = %q, want the change preview even under --dry-run", stdout.String())
	}
}

func TestSyncCmd_OutputPath_MergesAndBacksUpExistingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")
	original := []byte("- Media:\n    - Manual Entry:\n        href: https://example.com\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}
	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return services, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output-path", path, "--dry-run=false"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(got), "Manual Entry") {
		t.Errorf("hand-written entry was lost:\n%s", got)
	}
	if !strings.Contains(string(got), "Jellyfin") {
		t.Errorf("new service was not merged in:\n%s", got)
	}

	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("no .bak was created for an existing file: %v", err)
	}
	if string(backup) != string(original) {
		t.Errorf(".bak content = %q, want the pre-write original %q", backup, original)
	}
}

func TestSyncCmd_UnknownConflictValue(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")
	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return nil, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output-path", path, "--conflict", "ask-nicely"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() = nil, want an error for an unsupported --conflict value")
	}
	if !strings.Contains(err.Error(), "ask-nicely") {
		t.Errorf("error = %q, want it to mention the passed value", err.Error())
	}
}

func TestSyncCmd_ConflictFail_LeavesFileUntouched(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")
	// A marker claiming content the rendered entry doesn't match — as if
	// dashsync wrote something else here before and a human edited it.
	original := []byte("- Media:\n    # dashsync:managed id=id1 content=deadbeef\n    - Jellyfin:\n        href: http://HAND-EDITED\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}
	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return services, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output-path", path, "--conflict", "fail"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() = nil, want a conflict error")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != string(original) {
		t.Errorf("file was modified despite --conflict=fail:\ngot:\n%s\nwant (unchanged):\n%s", got, original)
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Error("no .bak should be created when the write never happened")
	}
}

func TestSyncCmd_DryRunDefaultsToTrueUnderCI(t *testing.T) {
	// Not t.Parallel(): t.Setenv forbids it.
	t.Setenv("CI", "true")

	path := filepath.Join(t.TempDir(), "services.yaml")
	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}

	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return services, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output-path", path}) // --dry-run not passed explicitly

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("under $CI, sync should default to --dry-run and not write")
	}
}

func TestSyncCmd_PassesHostAddrFlag(t *testing.T) {
	t.Parallel()

	var gotHostAddr string
	cmd := newSyncCmd(func(_ context.Context, hostAddr string) ([]model.Service, error) {
		gotHostAddr = hostAddr
		return nil, nil
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--host-addr", "10.0.0.5"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	if gotHostAddr != "10.0.0.5" {
		t.Errorf("discover was called with hostAddr %q, want %q", gotHostAddr, "10.0.0.5")
	}
}
