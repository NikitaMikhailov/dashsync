package discovery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

// fakeDockerClient implements DockerClient without a real daemon. It also
// implements Close, so multihost_test.go can reuse it wherever a full
// dockerClient (DockerClient plus Close) is needed instead of declaring a
// second, near-identical fake.
type fakeDockerClient struct {
	result client.ContainerListResult
	err    error
}

func (f fakeDockerClient) ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
	return f.result, f.err
}

func (f fakeDockerClient) Close() error { return nil }

func TestDiscover(t *testing.T) {
	t.Parallel()

	fake := fakeDockerClient{
		result: client.ContainerListResult{
			Items: []container.Summary{
				{
					Names:  []string{"/jellyfin"},
					Labels: map[string]string{"dashsync.enable": "true", "dashsync.group": "Media"},
					State:  container.StateRunning,
					Ports: []container.PortSummary{
						{PublicPort: 8096, Type: "tcp"},
						// An unpublished port alongside a published one:
						// exercises toPorts' filter through the full
						// Discover path, not just in isolation.
						{PublicPort: 0, Type: "tcp"},
					},
				},
				{
					// No dashsync.enable — must not show up in the result.
					Names:  []string{"/postgres"},
					Labels: map[string]string{},
					State:  container.StateRunning,
				},
				{
					// Enabled but stopped: must still show up, with its
					// status carried through, not hidden.
					Names:  []string{"/sonarr"},
					Labels: map[string]string{"dashsync.enable": "true", "dashsync.group": "Media"},
					State:  container.StateExited,
				},
				{
					Names:  []string{"/adguard"},
					Labels: map[string]string{"dashsync.enable": "true", "dashsync.group": "Network"},
					State:  container.StateRunning,
				},
			},
		},
	}

	got, err := Discover(context.Background(), Host{Name: "homelab-1", Client: fake}, "10.0.0.5")
	if err != nil {
		t.Fatalf("Discover() error = %v, want nil", err)
	}

	if len(got) != 3 {
		t.Fatalf("Discover() returned %d services, want 3 (postgres has no dashsync.enable): %+v", len(got), got)
	}

	// Sorted by (Group, Name, ID): Media/jellyfin, Media/sonarr, Network/adguard.
	wantOrder := []string{"jellyfin", "sonarr", "adguard"}
	for i, name := range wantOrder {
		if got[i].Name != name {
			t.Errorf("services[%d].Name = %q, want %q (order should be deterministic by Group then Name)", i, got[i].Name, name)
		}
	}

	if got[0].URL != "http://10.0.0.5:8096" {
		t.Errorf("jellyfin URL = %q, want auto-detected from its published port (the unpublished one should be ignored)", got[0].URL)
	}
	if got[0].Status != "running" {
		t.Errorf("jellyfin Status = %q, want %q", got[0].Status, "running")
	}

	wantID := model.Source{Host: "homelab-1", Container: "jellyfin"}.ID()
	if got[0].ID != wantID {
		t.Errorf("jellyfin ID = %q, want %q", got[0].ID, wantID)
	}

	if got[1].Status != "exited" {
		t.Errorf("sonarr (stopped) Status = %q, want %q — a stopped-but-enabled service must not be hidden", got[1].Status, "exited")
	}
}

func TestDiscover_ClientError(t *testing.T) {
	t.Parallel()

	fake := fakeDockerClient{err: errors.New("connection refused")}

	_, err := Discover(context.Background(), Host{Name: "homelab-1", Client: fake}, "10.0.0.5")
	if err == nil {
		t.Fatal("Discover() error = nil, want the ContainerList error wrapped")
	}
	if !strings.Contains(err.Error(), "homelab-1") {
		t.Errorf("error = %q, want it to name the host that failed", err.Error())
	}
	if !errors.Is(err, fake.err) {
		t.Errorf("error = %v, want it to wrap the original ContainerList error (errors.Is should see through it)", err)
	}
}

func TestToPorts(t *testing.T) {
	t.Parallel()

	got := toPorts([]container.PortSummary{
		{PublicPort: 8096, Type: "tcp"},
		{PublicPort: 0, Type: "tcp"}, // unpublished: must be dropped
		{PublicPort: 53, Type: "udp"},
	})

	want := []Port{
		{Public: 8096, Type: "tcp"},
		{Public: 53, Type: "udp"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("toPorts() mismatch (-want +got):\n%s", diff)
	}
}

func TestContainerName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		names []string
		want  string
	}{
		{name: "strips the leading slash", names: []string{"/jellyfin"}, want: "jellyfin"},
		{name: "no names at all", names: nil, want: ""},
		{name: "tolerates a name with no leading slash", names: []string{"jellyfin"}, want: "jellyfin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := containerName(tt.names); got != tt.want {
				t.Errorf("containerName(%v) = %q, want %q", tt.names, got, tt.want)
			}
		})
	}
}

func TestNewDockerClientForHost_EmptyAddressUsesEnvironment(t *testing.T) {
	// Not t.Parallel(): mutates Docker env vars via t.Setenv, which panics
	// if this test runs in parallel with another that also sets them.
	//
	// client.FromEnv reads more than DOCKER_HOST — DOCKER_TLS_VERIFY and
	// DOCKER_CERT_PATH, if set, make it try to load TLS material from a
	// path that won't exist in this test's environment, and would turn
	// this test flaky on any machine or runner where those happen to be
	// set in the ambient shell (a local dev machine with Docker Desktop's
	// TLS env sourced globally, say). Blanking them out — not just
	// DOCKER_HOST — is what actually pins down the "empty address" branch
	// regardless of the shell this test runs in.
	t.Setenv("DOCKER_HOST", "tcp://from-env:2376")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	t.Setenv("DOCKER_API_VERSION", "")

	c, err := NewDockerClientForHost("", "", "", "")
	if err != nil {
		t.Fatalf("NewDockerClientForHost() error = %v, want nil", err)
	}
	defer c.Close() //nolint:errcheck // test cleanup, nothing to act on

	if got := c.DaemonHost(); got != "tcp://from-env:2376" {
		t.Errorf("DaemonHost() = %q, want the value read from $DOCKER_HOST", got)
	}
}

func TestNewDockerClientForHost_ExplicitAddressOverridesEnvironment(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://from-env:2376")

	c, err := NewDockerClientForHost("tcp://10.0.0.6:2376", "", "", "")
	if err != nil {
		t.Fatalf("NewDockerClientForHost() error = %v, want nil", err)
	}
	defer c.Close() //nolint:errcheck // test cleanup, nothing to act on

	if got := c.DaemonHost(); got != "tcp://10.0.0.6:2376" {
		t.Errorf("DaemonHost() = %q, want the explicit address, not $DOCKER_HOST", got)
	}
}

func TestNewDockerClientForHost_NoTLSFieldsSetSucceeds(t *testing.T) {
	t.Parallel()

	// *client.Client exposes no accessor for its transport's TLS state, so
	// this can't directly assert "TLS is unconfigured" the way DaemonHost
	// lets the address-resolution tests assert their own outcome — the
	// closest honest check is that skipping WithTLSClientConfig entirely
	// (the "tlsCA/tlsCert/tlsKey all empty" branch in docker.go) doesn't
	// itself produce an error. TestNewDockerClientForHost_TLSFieldsAreActuallyApplied,
	// below, is what actually pins down that the tls fields reach the
	// option when they're set.
	c, err := NewDockerClientForHost("tcp://10.0.0.6:2376", "", "", "")
	if err != nil {
		t.Fatalf("NewDockerClientForHost() error = %v, want nil", err)
	}
	defer c.Close() //nolint:errcheck // test cleanup, nothing to act on
}

func TestNewDockerClientForHost_TLSFieldsAreActuallyApplied(t *testing.T) {
	t.Parallel()

	// A bad cert/key path only surfaces as an error if the tls fields
	// actually reach client.WithTLSClientConfig — proving the branch is
	// wired up without needing a real daemon or real certificates.
	_, err := NewDockerClientForHost("tcp://10.0.0.6:2376", "", "/no/such/cert.pem", "/no/such/key.pem")
	if err == nil {
		t.Fatal("NewDockerClientForHost() error = nil, want an error: the tls cert/key files don't exist")
	}
}
