package discovery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/NikitaMikhailov/dashsync/internal/config"
)

// connectCall records one dockerConnector invocation, for tests that need
// to assert what DiscoverAll actually passed through per host.
type connectCall struct {
	address, tlsCA, tlsCert, tlsKey string
}

// fakeConnector is a dockerConnector built from a table of per-address
// results, with an optional artificial delay to force completion order to
// differ from input order — DiscoverAll's ordering guarantee is only worth
// testing if some host can be made to finish before an earlier one.
type fakeConnector struct {
	byAddress map[string]connectorResult
	delay     map[string]time.Duration

	mu    sync.Mutex
	calls []connectCall
}

type connectorResult struct {
	client dockerClient
	err    error
}

func (f *fakeConnector) connect(_ context.Context, address, tlsCA, tlsCert, tlsKey string) (dockerClient, error) {
	f.mu.Lock()
	f.calls = append(f.calls, connectCall{address, tlsCA, tlsCert, tlsKey})
	f.mu.Unlock()

	if d, ok := f.delay[address]; ok {
		time.Sleep(d)
	}

	r, ok := f.byAddress[address]
	if !ok {
		return nil, errors.New("fakeConnector: no result configured for address " + address)
	}
	return r.client, r.err
}

func enabledContainer(name, group string) container.Summary {
	return container.Summary{
		Names:  []string{"/" + name},
		Labels: map[string]string{"dashsync.enable": "true", "dashsync.group": group},
		State:  container.StateRunning,
	}
}

func TestDiscoverAll_OrderMatchesHostsRegardlessOfCompletionOrder(t *testing.T) {
	t.Parallel()

	hosts := []config.Host{
		{Name: "slow", Address: "tcp://slow:2376"},
		{Name: "fast", Address: "tcp://fast:2376"},
	}
	fc := &fakeConnector{
		byAddress: map[string]connectorResult{
			"tcp://slow:2376": {client: fakeDockerClient{result: client.ContainerListResult{
				Items: []container.Summary{enabledContainer("on-slow", "Media")},
			}}},
			"tcp://fast:2376": {client: fakeDockerClient{result: client.ContainerListResult{
				Items: []container.Summary{enabledContainer("on-fast", "Media")},
			}}},
		},
		// "slow" takes longer to connect than "fast", so if DiscoverAll
		// just appended in completion order, "fast" would land first.
		delay: map[string]time.Duration{"tcp://slow:2376": 30 * time.Millisecond},
	}

	results := discoverAll(context.Background(), hosts, "localhost", fc.connect)

	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Host != "slow" || results[1].Host != "fast" {
		t.Errorf("results = [%q, %q], want [\"slow\", \"fast\"] — input order, not completion order",
			results[0].Host, results[1].Host)
	}
}

func TestDiscoverAll_IsolatesOneHostsConnectFailure(t *testing.T) {
	t.Parallel()

	hosts := []config.Host{
		{Name: "unreachable", Address: "tcp://10.0.0.9:2376"},
		{Name: "healthy", Address: "tcp://10.0.0.5:2376"},
	}
	fc := &fakeConnector{
		byAddress: map[string]connectorResult{
			"tcp://10.0.0.9:2376": {err: errors.New("connection refused")},
			"tcp://10.0.0.5:2376": {client: fakeDockerClient{result: client.ContainerListResult{
				Items: []container.Summary{enabledContainer("jellyfin", "Media")},
			}}},
		},
	}

	results := discoverAll(context.Background(), hosts, "localhost", fc.connect)

	if results[0].Err == nil {
		t.Error("results[0].Err = nil, want the connect failure for the unreachable host")
	}
	if len(results[0].Services) != 0 {
		t.Errorf("results[0].Services = %+v, want none for a host that never connected", results[0].Services)
	}
	if results[1].Err != nil {
		t.Errorf("results[1].Err = %v, want nil — the healthy host must not be affected by the other one failing", results[1].Err)
	}
	if len(results[1].Services) != 1 {
		t.Errorf("results[1].Services = %+v, want one service from the healthy host", results[1].Services)
	}
}

func TestDiscoverAll_IsolatesOneHostsContainerListFailure(t *testing.T) {
	t.Parallel()

	// Distinct from a connect failure: the client connects fine, but
	// listing its containers fails — Discover's own error path, reached
	// through DiscoverAll rather than called directly.
	hosts := []config.Host{
		{Name: "flaky", Address: "tcp://10.0.0.9:2376"},
		{Name: "healthy", Address: "tcp://10.0.0.5:2376"},
	}
	fc := &fakeConnector{
		byAddress: map[string]connectorResult{
			"tcp://10.0.0.9:2376": {client: fakeDockerClient{err: errors.New("api timeout")}},
			"tcp://10.0.0.5:2376": {client: fakeDockerClient{result: client.ContainerListResult{
				Items: []container.Summary{enabledContainer("jellyfin", "Media")},
			}}},
		},
	}

	results := discoverAll(context.Background(), hosts, "localhost", fc.connect)

	if results[0].Err == nil {
		t.Error("results[0].Err = nil, want the ContainerList failure surfaced")
	}
	if results[1].Err != nil || len(results[1].Services) != 1 {
		t.Errorf("results[1] = %+v, want the healthy host unaffected", results[1])
	}
}

func TestDiscoverAll_PassesTLSFieldsToConnector(t *testing.T) {
	t.Parallel()

	hosts := []config.Host{
		{
			Name:    "secure",
			Address: "tcp://10.0.0.6:2376",
			TLS:     &config.TLS{CA: "/certs/ca.pem", Cert: "/certs/cert.pem", Key: "/certs/key.pem"},
		},
		{Name: "plain", Address: "tcp://10.0.0.7:2376"},
	}
	fc := &fakeConnector{
		byAddress: map[string]connectorResult{
			"tcp://10.0.0.6:2376": {client: fakeDockerClient{}},
			"tcp://10.0.0.7:2376": {client: fakeDockerClient{}},
		},
	}

	discoverAll(context.Background(), hosts, "localhost", fc.connect)

	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2", len(fc.calls))
	}
	for _, c := range fc.calls {
		switch c.address {
		case "tcp://10.0.0.6:2376":
			if c.tlsCA != "/certs/ca.pem" || c.tlsCert != "/certs/cert.pem" || c.tlsKey != "/certs/key.pem" {
				t.Errorf("secure host tls fields = %+v, want the host's own tls block passed through", c)
			}
		case "tcp://10.0.0.7:2376":
			if c.tlsCA != "" || c.tlsCert != "" || c.tlsKey != "" {
				t.Errorf("plain host tls fields = %+v, want all empty (no tls: block configured)", c)
			}
		}
	}
}

func TestDiscoverAll_UsesPerHostURLResolution(t *testing.T) {
	t.Parallel()

	hosts := []config.Host{
		// No url_host, but the address has a hostname of its own.
		{Name: "homelab", Address: "tcp://10.0.0.6:2376"},
		// No address at all — must fall back to urlHostFallback.
		{Name: "local"},
	}
	fc := &fakeConnector{
		byAddress: map[string]connectorResult{
			"tcp://10.0.0.6:2376": {client: fakeDockerClient{result: client.ContainerListResult{
				Items: []container.Summary{{
					Names:  []string{"/svc"},
					Labels: map[string]string{"dashsync.enable": "true"},
					State:  container.StateRunning,
					Ports:  []container.PortSummary{{PublicPort: 80, Type: "tcp"}},
				}},
			}}},
			"": {client: fakeDockerClient{result: client.ContainerListResult{
				Items: []container.Summary{{
					Names:  []string{"/svc"},
					Labels: map[string]string{"dashsync.enable": "true"},
					State:  container.StateRunning,
					Ports:  []container.PortSummary{{PublicPort: 81, Type: "tcp"}},
				}},
			}}},
		},
	}

	results := discoverAll(context.Background(), hosts, "fallback.example", fc.connect)

	if got := results[0].Services[0].URL; got != "http://10.0.0.6:80" {
		t.Errorf("homelab service URL = %q, want it built from the address's own hostname", got)
	}
	if got := results[1].Services[0].URL; got != "http://fallback.example:81" {
		t.Errorf("local service URL = %q, want it built from urlHostFallback", got)
	}
}

func TestDiscoverAll_EmptyHosts(t *testing.T) {
	t.Parallel()

	results := discoverAll(context.Background(), nil, "localhost", (&fakeConnector{}).connect)

	if len(results) != 0 {
		t.Errorf("results = %+v, want none for an empty host list", results)
	}
}

func TestDiscoverAll_RealConnectorSurfacesUnreachableHostAsAnError(t *testing.T) {
	t.Parallel()

	// Exercises the real, exported DiscoverAll -> connectDocker ->
	// NewDockerClientForHost -> Discover -> ContainerList path end to end,
	// without a real Docker daemon — proving the whole chain the
	// fakeConnector-based tests above don't touch is actually wired
	// together correctly.
	//
	// Deliberately loopback, not an off-host address: an earlier version
	// of this test dialed 192.0.2.1 (RFC 5737, reserved for documentation)
	// expecting a fast "unreachable" failure, but that packet can be
	// silently dropped rather than rejected depending on the network path
	// — in this project's own sandboxed CI-like environment it blackholed
	// for the full length of whatever ctx timeout was set, rather than
	// failing fast, and a NAT or transparent proxy elsewhere could make it
	// behave differently again. 127.0.0.1 on a port nothing listens on
	// gets an immediate, OS-level connection-refused on every platform
	// this project builds for, regardless of the machine's network
	// environment or whether it has outbound access at all.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hosts := []config.Host{{Name: "unreachable", Address: "tcp://127.0.0.1:1"}}
	results := DiscoverAll(ctx, hosts, "localhost")

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Error("results[0].Err = nil, want a connection failure for an unreachable address")
	}
}
