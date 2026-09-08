// Package discovery reads container state from Docker and maps it into
// dashsync's canonical model. It never writes to Docker — every call it
// makes is one of the API's read endpoints.
package discovery

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

// DockerClient is the subset of the Docker API client discovery needs,
// declared on the consumer side so tests can substitute a fake instead of
// depending on a real daemon. *client.Client satisfies it without any
// adapter.
type DockerClient interface {
	ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error)
}

// Host bundles a named Docker endpoint with the client connected to it.
// Multi-host support (M4) will hold a slice of these; for now the CLI wires
// up exactly one.
type Host struct {
	Name   string
	Client DockerClient
}

// NewDockerClient connects to a Docker daemon using the standard
// environment variables (DOCKER_HOST, DOCKER_API_VERSION, ...). The
// returned client's Close method should be called once the caller is done
// with it.
//
// This is a thin wrapper with no logic of its own to unit-test — the same
// reasoning that leaves buildinfo.Get() untested while its resolve() helper
// is tested exhaustively applies here too. It's covered by manually running
// `dashsync inspect` against a real daemon instead.
func NewDockerClient() (*client.Client, error) {
	return NewDockerClientForHost("", "", "", "")
}

// NewDockerClientForHost connects to the Docker daemon at address, or to
// the standard environment variables (same resolution as NewDockerClient)
// if address is empty — the same "empty means use the environment"
// contract config.Host.Address documents. tlsCA/tlsCert/tlsKey are ignored
// when address is empty: an explicit address is what makes an explicit
// TLS configuration meaningful in the first place (see
// docs/decisions/004-multi-host-config-schema.md).
//
// Unlike NewDockerClient, this does have real branches — empty vs. explicit
// address, and TLS vs. no TLS — and client.New doesn't dial anything, so
// they're each unit-tested (via the resulting client's own DaemonHost(),
// and via a deliberately-bad TLS file path) without needing a real daemon.
// What isn't covered here is whether a *client.Client built this way can
// actually talk to a real Docker API — that's what the real-Docker
// integration tests are for.
func NewDockerClientForHost(address, tlsCA, tlsCert, tlsKey string) (*client.Client, error) {
	if address == "" {
		c, err := client.New(client.FromEnv)
		if err != nil {
			return nil, fmt.Errorf("connect to docker: %w", err)
		}
		return c, nil
	}

	opts := []client.Opt{client.WithHost(address)}
	if tlsCA != "" || tlsCert != "" || tlsKey != "" {
		opts = append(opts, client.WithTLSClientConfig(tlsCA, tlsCert, tlsKey))
	}
	c, err := client.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to docker host %q: %w", address, err)
	}
	return c, nil
}

// Discover lists containers on host — running and stopped alike, so an
// enabled-but-currently-down service still shows up with Status reflecting
// that, rather than vanishing from view — and maps the ones with
// dashsync.enable=true into Services. hostAddr is used for URL
// auto-detection; see ParseLabels.
//
// The returned slice is sorted by (Group, Name, ID) so two runs against an
// unchanged Docker state produce byte-identical output further down the
// pipeline — the map/reduce shape here mirrors architecture.md's
// discovery -> mapper stage, and determinism is the property the rest of
// dashsync is built to preserve.
func Discover(ctx context.Context, host Host, hostAddr string) ([]model.Service, error) {
	opts := client.ContainerListOptions{
		All: true,
		// The daemon supports filtering by exact label value, so containers
		// that never opted in don't even get unmarshalled — cheap now,
		// meaningful once M4 fans this out across several hosts.
		Filters: make(client.Filters).Add("label", "dashsync.enable=true"),
	}

	result, err := host.Client.ContainerList(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("list containers on %q: %w", host.Name, err)
	}

	services := make([]model.Service, 0, len(result.Items))
	for _, c := range result.Items {
		name := containerName(c.Names)
		svc, ok := ParseLabels(name, host.Name, string(c.State), c.Labels, toPorts(c.Ports), hostAddr)
		if !ok {
			continue
		}
		services = append(services, svc)
	}

	SortServices(services)
	return services, nil
}

// SortServices sorts services by (Group, Name, ID) in place. Discover uses
// it to make one host's result deterministic; DiscoverAll's caller uses the
// same function to re-sort after concatenating several already-sorted
// per-host results, since concatenation alone doesn't interleave them into
// one globally sorted order.
func SortServices(services []model.Service) {
	slices.SortFunc(services, func(a, b model.Service) int {
		if c := cmp.Compare(a.Group, b.Group); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
}

// containerName returns a container's primary name with Docker's leading
// "/" stripped off. The API always returns names like "/my-container" —
// the slash is a holdover from legacy container linking, where a container
// could have more than one name; ContainerList always returns at least one
// entry.
func containerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func toPorts(ports []container.PortSummary) []Port {
	out := make([]Port, 0, len(ports))
	for _, p := range ports {
		if p.PublicPort == 0 {
			continue // not published to the host, nothing to link to
		}
		out = append(out, Port{Public: p.PublicPort, Type: p.Type})
	}
	return out
}
