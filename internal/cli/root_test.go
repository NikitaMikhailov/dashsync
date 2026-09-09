package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/NikitaMikhailov/dashsync/internal/discovery"
	"github.com/NikitaMikhailov/dashsync/internal/model"
)

func TestNewRootCmd_RegistersSubcommands(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()

	registered := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		registered[sub.Name()] = true
	}

	for _, want := range []string{"version", "inspect", "sync"} {
		if !registered[want] {
			t.Errorf("NewRootCmd() does not register a %q subcommand", want)
		}
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	got := Run([]string{"no-such-command"}, &stdout, &stderr)

	if got != 1 {
		t.Errorf("Run() exit code = %d, want 1 (stderr: %s)", got, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("stderr = %q, want it to mention the unknown command", stderr.String())
	}
}

func TestRun_Help(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	got := Run([]string{"--help"}, &stdout, &stderr)

	if got != 0 {
		t.Errorf("Run() exit code = %d, want 0 (stderr: %s)", got, stderr.String())
	}
	if !strings.Contains(stdout.String(), "dashsync") {
		t.Errorf("stdout = %q, want usage text mentioning dashsync", stdout.String())
	}
}

func TestDiscoverDocker_InvalidConfigIsAFatalError(t *testing.T) {
	t.Parallel()

	// config.Load's own validation (an empty "hosts" list) must propagate
	// as discoverDocker's fatal error, returned before discovery.DiscoverAll
	// is ever called — this doesn't need a real Docker daemon to exercise,
	// unlike the connect/discover happy path (see integration_test.go).
	path := filepath.Join(t.TempDir(), "dashsync.yaml")
	if err := os.WriteFile(path, []byte("hosts: []\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	services, warnings, err := discoverDocker(context.Background(), "localhost", path)
	if err == nil {
		t.Fatal("discoverDocker() error = nil, want config.Load's validation error to propagate")
	}
	if services != nil || warnings != nil {
		t.Errorf("discoverDocker() = (%v, %v), want (nil, nil) — nothing was even attempted", services, warnings)
	}
}

func TestAggregateHostResults_AllSucceed(t *testing.T) {
	t.Parallel()

	results := []discovery.HostResult{
		{Host: "b", Services: []model.Service{{Name: "sonarr", Group: "Media"}}},
		{Host: "a", Services: []model.Service{{Name: "jellyfin", Group: "Media"}}},
	}

	services, warnings, err := aggregateHostResults(results)
	if err != nil {
		t.Fatalf("aggregateHostResults() error = %v, want nil", err)
	}
	if warnings != nil {
		t.Errorf("warnings = %v, want none", warnings)
	}

	// Sorted by (Group, Name, ID) across hosts, not by which host answered
	// first or which host was listed first.
	want := []model.Service{{Name: "jellyfin", Group: "Media"}, {Name: "sonarr", Group: "Media"}}
	if diff := cmp.Diff(want, services); diff != "" {
		t.Errorf("services mismatch (-want +got):\n%s", diff)
	}
}

func TestAggregateHostResults_OneHostFailsBecomesAWarningNotAnError(t *testing.T) {
	t.Parallel()

	hostErr := errors.New("connection refused")
	results := []discovery.HostResult{
		{Host: "unreachable", Err: hostErr},
		{Host: "healthy", Services: []model.Service{{Name: "jellyfin", Group: "Media"}}},
	}

	services, warnings, err := aggregateHostResults(results)
	if err != nil {
		t.Fatalf("aggregateHostResults() error = %v, want nil — one of two hosts still succeeded", err)
	}
	if len(services) != 1 {
		t.Errorf("services = %+v, want the healthy host's one service", services)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	if !errors.Is(warnings[0], hostErr) {
		t.Errorf("warnings[0] = %v, want it to wrap the original host error", warnings[0])
	}
	if !strings.Contains(warnings[0].Error(), "unreachable") {
		t.Errorf("warnings[0] = %q, want it to name the failed host", warnings[0])
	}
}

func TestAggregateHostResults_AllHostsFailIsAFatalError(t *testing.T) {
	t.Parallel()

	results := []discovery.HostResult{
		{Host: "a", Err: errors.New("timeout")},
		{Host: "b", Err: errors.New("connection refused")},
	}

	services, warnings, err := aggregateHostResults(results)
	if err == nil {
		t.Fatal("aggregateHostResults() error = nil, want an error: every configured host failed")
	}
	if services != nil {
		t.Errorf("services = %+v, want none", services)
	}
	if len(warnings) != 2 {
		t.Errorf("warnings = %v, want both host failures reported even though the overall call also errors", warnings)
	}
}

func TestAggregateHostResults_NoHostsIsNotTreatedAsAllFailed(t *testing.T) {
	t.Parallel()

	// An empty result set isn't "every host failed" (there were none to
	// fail) — in practice config.validate() never allows zero hosts, but
	// aggregateHostResults shouldn't rely on that to avoid a misleading
	// "all 0 configured host(s) failed" error.
	services, warnings, err := aggregateHostResults(nil)
	if err != nil {
		t.Errorf("aggregateHostResults(nil) error = %v, want nil", err)
	}
	if services != nil || warnings != nil {
		t.Errorf("aggregateHostResults(nil) = (%v, %v), want (nil, nil)", services, warnings)
	}
}

func TestExitCodeFor_PendingChangesGetsItsOwnCode(t *testing.T) {
	t.Parallel()

	err := &pendingChangesError{count: 2, path: "services.yaml"}
	if got := exitCodeFor(err); got != exitPendingChanges {
		t.Errorf("exitCodeFor(pendingChangesError) = %d, want %d", got, exitPendingChanges)
	}
}

func TestExitCodeFor_EverythingElseGetsExitOne(t *testing.T) {
	t.Parallel()

	if got := exitCodeFor(errors.New("boom")); got != 1 {
		t.Errorf("exitCodeFor(plain error) = %d, want 1 (only pendingChangesError should get %d)", got, exitPendingChanges)
	}
}

func TestExitCodeFor_WrappedPendingChangesErrorStillMatches(t *testing.T) {
	t.Parallel()

	// fmt.Errorf("...: %w", ...) is how an error would realistically reach
	// Run() through cobra's own layers — errors.As, not a bare type
	// assertion, is what has to do the matching for this to work in
	// practice, not just against the unwrapped error directly.
	wrapped := fmt.Errorf("sync: %w", &pendingChangesError{count: 1, path: "x.yaml"})
	if got := exitCodeFor(wrapped); got != exitPendingChanges {
		t.Errorf("exitCodeFor(wrapped pendingChangesError) = %d, want %d", got, exitPendingChanges)
	}
}
