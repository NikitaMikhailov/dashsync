package discovery

import (
	"context"
	"sync"

	"github.com/NikitaMikhailov/dashsync/internal/config"
	"github.com/NikitaMikhailov/dashsync/internal/model"
)

// HostResult is one configured host's outcome from DiscoverAll: either
// Services is populated and Err is nil, or Err explains why that host
// couldn't be discovered from at all — connecting to it and listing its
// containers are both folded into this one failure mode, since either one
// means dashsync has nothing usable from that host this run.
type HostResult struct {
	Host     string
	Services []model.Service
	Err      error
}

// dockerClient is what DiscoverAll needs from a connected client beyond
// DockerClient itself: releasing the connection once it's done with it.
type dockerClient interface {
	DockerClient
	Close() error
}

// dockerConnector connects to one host's Docker daemon. Declared here, on
// the consumer side, purely so tests can substitute a fake instead of
// dialing a real address — the same reasoning DockerClient itself exists
// for in docker.go. connectDocker is the only production implementation.
//
// ctx is first, per this project's own convention, even though
// NewDockerClientForHost doesn't currently do anything cancellable with it
// (client.New builds an HTTP transport and reads TLS cert files; it
// doesn't dial): a connector is exactly the kind of thing a future
// implementation (an SSH tunnel dial, say) would need to bound by the
// caller's deadline, and a signature that already carries ctx means that
// day doesn't require another breaking change here.
type dockerConnector func(ctx context.Context, address, tlsCA, tlsCert, tlsKey string) (dockerClient, error)

func connectDocker(_ context.Context, address, tlsCA, tlsCert, tlsKey string) (dockerClient, error) {
	return NewDockerClientForHost(address, tlsCA, tlsCert, tlsKey)
}

// DiscoverAll discovers from every host in hosts concurrently, isolating
// each host's failure so one unreachable Docker daemon doesn't stop the
// others from being discovered. Results are returned in hosts' own order,
// not completion order, so the aggregate stays deterministic regardless of
// which host answers first — the caller still needs to re-sort after
// flattening every result's Services together, though: concatenating
// several already-sorted per-host slices doesn't interleave them into one
// globally sorted order (see SortServices).
//
// urlHostFallback is used for any host whose config sets neither url_host
// nor an Address with a resolvable hostname — see config.Host.ResolveURLHost.
func DiscoverAll(ctx context.Context, hosts []config.Host, urlHostFallback string) []HostResult {
	return discoverAll(ctx, hosts, urlHostFallback, connectDocker)
}

func discoverAll(ctx context.Context, hosts []config.Host, urlHostFallback string, connect dockerConnector) []HostResult {
	results := make([]HostResult, len(hosts))

	var wg sync.WaitGroup
	wg.Add(len(hosts))
	for i, h := range hosts {
		go func() {
			defer wg.Done()
			results[i] = discoverOneHost(ctx, h, urlHostFallback, connect)
		}()
	}
	wg.Wait()

	return results
}

func discoverOneHost(ctx context.Context, h config.Host, urlHostFallback string, connect dockerConnector) HostResult {
	var tlsCA, tlsCert, tlsKey string
	if h.TLS != nil {
		tlsCA, tlsCert, tlsKey = h.TLS.CA, h.TLS.Cert, h.TLS.Key
	}

	client, err := connect(ctx, h.Address, tlsCA, tlsCert, tlsKey)
	if err != nil {
		return HostResult{Host: h.Name, Err: err}
	}
	//nolint:errcheck // closing a client we're about to discard: nothing
	// actionable to do with a Close error here, same as discoverDocker's
	// own single-host Close in internal/cli/root.go.
	defer client.Close()

	services, err := Discover(ctx, Host{Name: h.Name, Client: client}, h.ResolveURLHost(urlHostFallback))
	if err != nil {
		return HostResult{Host: h.Name, Err: err}
	}
	return HostResult{Host: h.Name, Services: services}
}
