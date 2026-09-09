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

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
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

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
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

func TestSyncCmd_RendersHomerToStdout(t *testing.T) {
	t.Parallel()

	services := []model.Service{{Name: "Jellyfin", Group: "Media", URL: "http://10.0.0.5:8096"}}

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--format", "homer"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	// Decode structurally, same as TestSyncCmd_GroupsBeforeRendering above:
	// a substring check on "name: Jellyfin" would also pass for a
	// duplicated group, a missing "services:" wrapper, or arbitrary noise
	// around that one line, and wouldn't even confirm the output parses.
	var decoded struct {
		Services []struct {
			Name  string           `yaml:"name"`
			Items []map[string]any `yaml:"items"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("stdout did not parse as YAML: %v\nstdout:\n%s", err, stdout.String())
	}

	if len(decoded.Services) != 1 || decoded.Services[0].Name != "Media" {
		t.Fatalf("decoded = %+v, want one group named Media", decoded)
	}
	want := []map[string]any{{"name": "Jellyfin", "url": "http://10.0.0.5:8096"}}
	if diff := cmp.Diff(want, decoded.Services[0].Items); diff != "" {
		t.Errorf("items mismatch (-want +got):\n%s", diff)
	}
}

func TestSyncCmd_RendersDashyToStdout(t *testing.T) {
	t.Parallel()

	services := []model.Service{{Name: "Jellyfin", Group: "Media", URL: "http://10.0.0.5:8096"}}

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--format", "dashy"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	var decoded struct {
		Sections []struct {
			Name  string           `yaml:"name"`
			Items []map[string]any `yaml:"items"`
		} `yaml:"sections"`
	}
	if err := yaml.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("stdout did not parse as YAML: %v\nstdout:\n%s", err, stdout.String())
	}

	if len(decoded.Sections) != 1 || decoded.Sections[0].Name != "Media" {
		t.Fatalf("decoded = %+v, want one section named Media", decoded)
	}
	want := []map[string]any{{"title": "Jellyfin", "url": "http://10.0.0.5:8096"}}
	if diff := cmp.Diff(want, decoded.Sections[0].Items); diff != "" {
		t.Errorf("items mismatch (-want +got):\n%s", diff)
	}
}

func TestSyncCmd_DashyOutputPath_WritesAndIsIdempotent(t *testing.T) {
	t.Parallel()

	// Dashy gained merge.EntryRenderer support via NamedGroupAdapter (see
	// docs/decisions/007-document-adapter.md) — --output-path now works,
	// matching Homepage's existing behavior, not the error it used to
	// return.
	path := filepath.Join(t.TempDir(), "conf.yml")
	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}
	discover := func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil }

	cmd := newSyncCmd(discover)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--format", "dashy", "--output-path", path, "--dry-run=false"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("sync did not create %s: %v", path, err)
	}
	if !strings.Contains(string(first), "Jellyfin") || !strings.Contains(string(first), "sections:") {
		t.Errorf("written file = %q, want a Dashy-shaped sections list containing Jellyfin", first)
	}
	if !strings.Contains(stdout.String(), "added") {
		t.Errorf("stdout = %q, want a change summary mentioning \"added\"", stdout.String())
	}

	// Second run against unchanged Docker state: no changes reported, byte-
	// identical file.
	cmd2 := newSyncCmd(discover)
	var stdout2 bytes.Buffer
	cmd2.SetOut(&stdout2)
	cmd2.SetArgs([]string{"--format", "dashy", "--output-path", path, "--dry-run=false"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("second Execute() = %v, want nil", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s after second run: %v", path, err)
	}
	if string(first) != string(second) {
		t.Errorf("second run changed the file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if strings.Contains(stdout2.String(), "added") || strings.Contains(stdout2.String(), "updated") {
		t.Errorf("stdout on unchanged input = %q, want no changes reported", stdout2.String())
	}
}

func TestSyncCmd_HomerOutputPath_WritesAndIsIdempotent(t *testing.T) {
	t.Parallel()

	// Homer gained merge.EntryRenderer support via NamedGroupAdapter (see
	// docs/decisions/007-document-adapter.md) — --output-path now works,
	// matching Homepage's existing behavior, not the error it used to
	// return.
	path := filepath.Join(t.TempDir(), "config.yml")
	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}
	discover := func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil }

	cmd := newSyncCmd(discover)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--format", "homer", "--output-path", path, "--dry-run=false"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("sync did not create %s: %v", path, err)
	}
	if !strings.Contains(string(first), "Jellyfin") || !strings.Contains(string(first), "services:") {
		t.Errorf("written file = %q, want a Homer-shaped services list containing Jellyfin", first)
	}
	if !strings.Contains(stdout.String(), "added") {
		t.Errorf("stdout = %q, want a change summary mentioning \"added\"", stdout.String())
	}

	// Second run against unchanged Docker state: no changes reported, byte-
	// identical file.
	cmd2 := newSyncCmd(discover)
	var stdout2 bytes.Buffer
	cmd2.SetOut(&stdout2)
	cmd2.SetArgs([]string{"--format", "homer", "--output-path", path, "--dry-run=false"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("second Execute() = %v, want nil", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s after second run: %v", path, err)
	}
	if string(first) != string(second) {
		t.Errorf("second run changed the file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if strings.Contains(stdout2.String(), "added") || strings.Contains(stdout2.String(), "updated") {
		t.Errorf("stdout on unchanged input = %q, want no changes reported", stdout2.String())
	}
}

func TestSyncCmd_UnknownFormat(t *testing.T) {
	t.Parallel()

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return nil, nil, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--format", "no-such-format"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() = nil, want an error for an unsupported --format value")
	}
	if !strings.Contains(err.Error(), "no-such-format") {
		t.Errorf("error = %q, want it to mention the passed value %q", err.Error(), "no-such-format")
	}
}

func TestSyncCmd_DiscoveryError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connect to docker: no such host")
	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return nil, nil, wantErr })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(nil)

	if err := cmd.Execute(); !errors.Is(err, wantErr) {
		t.Errorf("Execute() error = %v, want it to be (or wrap) %v", err, wantErr)
	}
}

func TestSyncCmd_OutputPath_FailsCleanlyWhenAnotherWriterHoldsTheLock(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")
	original := []byte("- Media:\n    - Manual Entry:\n        href: https://example.com\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// Simulate a second dashsync process already writing to this file.
	release, err := acquireLock(path, true)
	if err != nil {
		t.Fatalf("acquireLock() = %v, want nil", err)
	}
	t.Cleanup(func() { _ = release() })

	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}
	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output-path", path, "--dry-run=false"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() = nil, want an error: the lock is already held")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != string(original) {
		t.Errorf("file was modified despite the lock being held by someone else:\ngot:\n%s\nwant (unchanged):\n%s", got, original)
	}
}

func TestSyncCmd_OutputPath_WriteFailsWhileACheckRunHoldsTheSharedLock(t *testing.T) {
	t.Parallel()

	// The asymmetric case that actually matters in production: a
	// scheduled --check overlapping a scheduled real write must still
	// stop the write, even though two --check runs (or a --check racing
	// each other) must not stop each other — see acquireLock's own doc
	// comment. This is the RunE-level proof that --check really does
	// take the lock (shared, but still a lock) rather than skipping
	// locking altogether for read-only invocations.
	path := filepath.Join(t.TempDir(), "services.yaml")
	original := []byte("- Media:\n    - Manual Entry:\n        href: https://example.com\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// Simulate a concurrent --check run: a shared hold, not exclusive.
	release, err := acquireLock(path, false)
	if err != nil {
		t.Fatalf("acquireLock(shared) = %v, want nil", err)
	}
	t.Cleanup(func() { _ = release() })

	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}
	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output-path", path, "--dry-run=false"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() = nil, want an error: a real write must not proceed while a --check run holds the shared lock")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != string(original) {
		t.Errorf("file was modified despite a concurrent --check holding the lock:\ngot:\n%s\nwant (unchanged):\n%s", got, original)
	}
}

func TestSyncCmd_Check_DoesNotBlockAnotherConcurrentCheck(t *testing.T) {
	t.Parallel()

	// The other half of the asymmetry: two read-only runs must not fail
	// each other, or --check would be useless the moment two overlapping
	// schedules both happened to be --check.
	path := filepath.Join(t.TempDir(), "services.yaml")
	original := []byte("- Media:\n    - Manual Entry:\n        href: https://example.com\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	release, err := acquireLock(path, false)
	if err != nil {
		t.Fatalf("acquireLock(shared) = %v, want nil", err)
	}
	t.Cleanup(func() { _ = release() })

	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}
	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output-path", path, "--check"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() = nil, want an error: this file has a pending change, --check should still report it")
	} else if strings.Contains(err.Error(), "locked") {
		t.Errorf("Execute() error = %v, want the pending-changes error, not a lock conflict against another --check run", err)
	}
}

func TestSyncCmd_OutputPath_WritesFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")
	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
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
	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
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

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
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

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
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
	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
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

func TestSyncCmd_Check_FailsWhenChangesArePending(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")
	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--output-path", path, "--check"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() = nil, want an error: a fresh file has a pending 'added' change")
	}
	if !strings.Contains(stdout.String(), "added") {
		t.Errorf("stdout = %q, want the change preview before the error", stdout.String())
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("--check must never write, even when it reports pending changes")
	}
}

func TestSyncCmd_Check_WithConflictFail_ReportsTheConflictNotPendingChanges(t *testing.T) {
	t.Parallel()

	// merge.Merge returns a *ConflictError under --conflict=fail before
	// the changes slice --check inspects even exists (internal/merge/
	// merge.go's Fail case returns from inside Merge itself) — so --check
	// composed with --conflict=fail must surface as that same conflict
	// failure, not as --check's own "N pending change(s)" message, and
	// must still leave the file untouched either way.
	path := filepath.Join(t.TempDir(), "services.yaml")
	original := []byte("- Media:\n    # dashsync:managed id=id1 content=deadbeef\n    - Jellyfin:\n        href: http://HAND-EDITED\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}
	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output-path", path, "--conflict", "fail", "--check"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() = nil, want a conflict error")
	}
	if strings.Contains(err.Error(), "pending change") {
		t.Errorf("error = %q, want the conflict error, not --check's own pending-changes message", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != string(original) {
		t.Errorf("file was modified despite --conflict=fail --check:\ngot:\n%s\nwant (unchanged):\n%s", got, original)
	}
}

func TestSyncCmd_Check_FailsOnARemovedServiceToo(t *testing.T) {
	t.Parallel()

	// TestSyncCmd_Check_FailsWhenChangesArePending only ever exercises an
	// Added change; len(changes) > 0 is kind-agnostic, but a regression
	// that special-cased Added wouldn't be caught without this.
	path := filepath.Join(t.TempDir(), "services.yaml")
	// The content hash doesn't need to be real: removal doesn't check it
	// (ADR 002 — a managed entry whose ID is gone from Desired is removed
	// unconditionally, hand-edited or not), only the marker's id matters.
	original := []byte("- Media:\n    # dashsync:managed id=id1 content=deadbeef\n    - Jellyfin:\n        href: http://x\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// The container behind id1 is gone from this run's discovery.
	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return nil, nil, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--output-path", path, "--check"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() = nil, want an error: the file has a pending removal")
	}
	if !strings.Contains(stdout.String(), "removed") {
		t.Errorf("stdout = %q, want the removal reflected in the preview", stdout.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != string(original) {
		t.Error("--check must not remove the entry, only report that it would be removed")
	}
}

func TestSyncCmd_Check_SucceedsWhenNothingIsPending(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")
	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}

	// First write the file for real, so the second, --check run has
	// nothing left to do.
	seed := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
	seed.SetOut(&bytes.Buffer{})
	seed.SetArgs([]string{"--output-path", path, "--dry-run=false"})
	if err := seed.Execute(); err != nil {
		t.Fatalf("seed Execute() = %v, want nil", err)
	}

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--output-path", path, "--check"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil: the file already matches, nothing is pending", err)
	}
	if !strings.Contains(stdout.String(), "no changes") {
		t.Errorf("stdout = %q, want %q", stdout.String(), "no changes")
	}
}

func TestSyncCmd_Check_RequiresOutputPath(t *testing.T) {
	t.Parallel()

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return nil, nil, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() = nil, want an error: --check has nothing to compare against without --output-path")
	}
	if !strings.Contains(err.Error(), "--output-path") {
		t.Errorf("error = %q, want it to mention --output-path", err)
	}
}

func TestSyncCmd_Check_IgnoresDryRunFalse(t *testing.T) {
	t.Parallel()

	// --check's whole point is never writing regardless of what triggered
	// it — an explicit --dry-run=false must not override that.
	path := filepath.Join(t.TempDir(), "services.yaml")
	services := []model.Service{{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://x"}}

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output-path", path, "--check", "--dry-run=false"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() = nil, want an error: a fresh file has a pending change")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("--check must not write even with --dry-run=false")
	}
}

func TestSyncCmd_UnknownConflictValue(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")
	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return nil, nil, nil })
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
	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
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

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
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
	cmd := newSyncCmd(func(_ context.Context, hostAddr, _ string) ([]model.Service, []error, error) {
		gotHostAddr = hostAddr
		return nil, nil, nil
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

func TestSyncCmd_PassesConfigFlag(t *testing.T) {
	t.Parallel()

	var gotConfigPath string
	cmd := newSyncCmd(func(_ context.Context, _, configPath string) ([]model.Service, []error, error) {
		gotConfigPath = configPath
		return nil, nil, nil
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--config", "/etc/dashsync/hosts.yaml"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	if gotConfigPath != "/etc/dashsync/hosts.yaml" {
		t.Errorf("discover was called with configPath %q, want %q", gotConfigPath, "/etc/dashsync/hosts.yaml")
	}
}

func TestSyncCmd_PrintsPerHostWarningsToStderr(t *testing.T) {
	t.Parallel()

	services := []model.Service{{Name: "Jellyfin", Group: "Media", URL: "http://10.0.0.5:8096"}}
	warnings := []error{errors.New(`host "flaky": connection refused`)}

	cmd := newSyncCmd(func(context.Context, string, string) ([]model.Service, []error, error) {
		return services, warnings, nil
	})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil — a warning must not fail the command", err)
	}
	if !strings.Contains(stderr.String(), "flaky") {
		t.Errorf("stderr = %q, want the per-host warning printed", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Jellyfin") {
		t.Errorf("stdout = %q, want the rendered output from the hosts that did succeed", stdout.String())
	}
}
