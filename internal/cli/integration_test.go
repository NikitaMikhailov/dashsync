//go:build integration

// This file only builds under the "integration" tag (`go test
// -tags=integration ./internal/cli/...`) — it needs a real, reachable
// Docker daemon and is excluded from the ordinary `go test ./...` every
// other package's tests run under, the same way PROJECT_PLAN.md's testing
// section always intended for the eventual real-Docker test tier.
//
// It automates exactly the manual verification every milestone up to M3
// was checked with by hand — a real container, a real `sync`, a real
// diff — as an actual CI job instead of a step someone has to remember to
// run locally.
package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NikitaMikhailov/dashsync/internal/cli"
)

// dockerCmdTimeout bounds every docker CLI invocation this test shells
// out to — a stuck `docker run` (a slow image pull with no network, a
// wedged daemon) should fail the test loudly rather than hang the CI job
// until the runner's own overall timeout kills it with a far less useful
// error.
const dockerCmdTimeout = 60 * time.Second

// runDocker runs `docker <args...>`, bounded by dockerCmdTimeout, and
// returns its combined output.
func runDocker(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dockerCmdTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
}

// TestE2E_DockerToServicesYAML exercises the full discovery → sync →
// idempotent-merge pipeline against a real Docker daemon: two labeled
// containers, a hand-written entry already in the target file, and then
// three `sync` runs checking (1) the initial merge, (2) idempotency
// against unchanged Docker state, and (3) removal once a container goes
// away — the three properties this whole project exists to guarantee, now
// checked against something dashsync doesn't control at all, not just
// fakes internal/discovery and internal/merge's own unit tests construct.
func TestE2E_DockerToServicesYAML(t *testing.T) {
	requireDocker(t)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	jellyfinName := "dashsync-e2e-jellyfin-" + suffix
	adguardName := "dashsync-e2e-adguard-" + suffix

	runContainer(t, jellyfinName, "18096", map[string]string{
		"dashsync.enable": "true",
		"dashsync.group":  "Media",
		"dashsync.name":   "Jellyfin",
	})
	runContainer(t, adguardName, "18053", map[string]string{
		"dashsync.enable": "true",
		"dashsync.group":  "Network",
		"dashsync.name":   "AdGuard Home",
	})

	path := filepath.Join(t.TempDir(), "services.yaml")
	seed := "- Media:\n    - Manual Entry:\n        href: https://example.com\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}

	out, code := runSync(t, path)
	if code != 0 {
		t.Fatalf("first sync exit code = %d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "added") {
		t.Errorf("first sync output = %q, want it to report additions", out)
	}

	first := readFile(t, path)
	for _, want := range []string{"Manual Entry", "Jellyfin", "AdGuard Home"} {
		if !strings.Contains(first, want) {
			t.Errorf("first sync result is missing %q:\n%s", want, first)
		}
	}

	// Idempotency: a second run against unchanged Docker state changes
	// nothing at all.
	out2, code2 := runSync(t, path)
	if code2 != 0 {
		t.Fatalf("second sync exit code = %d, output:\n%s", code2, out2)
	}
	if !strings.Contains(out2, "no changes") {
		t.Errorf("second sync output = %q, want %q", out2, "no changes")
	}
	if second := readFile(t, path); first != second {
		t.Errorf("two runs against unchanged Docker state produced different output:\nrun 1:\n%s\nrun 2:\n%s", first, second)
	}

	// Removal: stopping one container should remove only its own entry.
	stopContainer(t, adguardName)

	out3, code3 := runSync(t, path)
	if code3 != 0 {
		t.Fatalf("third sync exit code = %d, output:\n%s", code3, out3)
	}
	if !strings.Contains(out3, "removed") {
		t.Errorf("third sync output = %q, want it to report a removal", out3)
	}

	third := readFile(t, path)
	if strings.Contains(third, "AdGuard Home") {
		t.Errorf("removed container's entry should be gone:\n%s", third)
	}
	for _, want := range []string{"Manual Entry", "Jellyfin"} {
		if !strings.Contains(third, want) {
			t.Errorf("unrelated entry %q should survive the removal:\n%s", want, third)
		}
	}
}

func requireDocker(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found on PATH — skipping integration test")
	}
	if _, err := runDocker("info"); err != nil {
		t.Skip("docker daemon not reachable — skipping integration test")
	}
}

func runContainer(t *testing.T, name, hostPort string, labels map[string]string) {
	t.Helper()

	args := []string{"run", "-d", "--rm", "--name", name, "-p", hostPort + ":80"}
	for k, v := range labels {
		args = append(args, "-l", k+"="+v)
	}
	// alpine, not something heavier: this container never needs to serve
	// anything — the -p flag registers the port mapping Docker reports
	// through ContainerList regardless of whether anything inside is
	// listening, which is all discovery's URL auto-detection depends on.
	args = append(args, "alpine:3.20", "sleep", "3600")

	t.Cleanup(func() { _, _ = runDocker("rm", "-f", name) })

	if out, err := runDocker(args...); err != nil {
		t.Fatalf("docker run %s: %v\n%s", name, err, out)
	}
}

func stopContainer(t *testing.T, name string) {
	t.Helper()

	if out, err := runDocker("rm", "-f", name); err != nil {
		t.Fatalf("docker rm -f %s: %v\n%s", name, err, out)
	}
}

// runSync invokes the real cli.Run entry point — not a helper reaching
// into internal/discovery or internal/merge directly — so this test
// exercises the exact code path a user's own `dashsync sync` does.
func runSync(t *testing.T, path string) (output string, exitCode int) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	// --dry-run=false explicitly: sync's own default is CI-env-aware (see
	// newSyncCmd), and this test runs as part of CI — a real write is
	// exactly what's under test here, not the safer default a human
	// forgetting the flag on a real cron job benefits from.
	code := cli.Run([]string{"sync", "--output-path", path, "--host-addr", "127.0.0.1", "--dry-run=false"}, &stdout, &stderr)
	if stderr.Len() > 0 {
		t.Logf("stderr: %s", stderr.String())
	}
	return stdout.String(), code
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
