// Package cli holds dashsync's cobra wiring: building the command tree and
// the entry point cmd/dashsync/main.go calls into.
package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/NikitaMikhailov/dashsync/internal/buildinfo"
	"github.com/NikitaMikhailov/dashsync/internal/config"
	"github.com/NikitaMikhailov/dashsync/internal/discovery"
	"github.com/NikitaMikhailov/dashsync/internal/model"
)

// dockerCallTimeout bounds how long discovery is allowed to take across
// every configured host combined, not per host: discoverDocker wraps this
// one deadline around the whole discovery.DiscoverAll call, whose hosts all
// run concurrently, so it reads as "the slowest of N hosts must answer
// within this long," not "each host individually gets this long." Without
// it, a stale socket or a hung daemon leaves `inspect` blocked forever with
// no feedback — an infrastructure CLI shouldn't have an unbounded wait
// against an external dependency as its only failure mode. A fixed budget
// shared across hosts is the simple choice for now; a per-host override
// would need its own config.Host field (like TLS or URLHost already have)
// if host count or per-host latency ever makes 10s too tight in practice.
const dockerCallTimeout = 10 * time.Second

// defaultHostAddr is the fallback for --host-addr on every command that
// takes it (inspect, sync): the host or IP used to build a URL
// auto-detected from a container's published ports, for any host whose
// config doesn't already resolve one — see config.Host.ResolveURLHost.
const defaultHostAddr = "localhost"

// defaultConfigPath is where --config looks for a multi-host config file
// if it isn't set. A missing file at this path is not an error — see
// config.Load — so a zero-config `dashsync sync` keeps discovering from
// just the local daemon, same as before multi-host support existed.
const defaultConfigPath = "dashsync.yaml"

// addHostAddrFlag registers --host-addr identically on every command that
// needs it. Defined once so inspect and sync can't quietly drift apart on
// the flag's default or help text.
func addHostAddrFlag(cmd *cobra.Command, hostAddr *string) {
	cmd.Flags().StringVar(hostAddr, "host-addr", defaultHostAddr,
		"host or IP used to build URLs auto-detected from published ports")
}

// addConfigFlag registers --config identically on every command that needs
// it, for the same reason addHostAddrFlag does.
func addConfigFlag(cmd *cobra.Command, configPath *string) {
	cmd.Flags().StringVar(configPath, "config", defaultConfigPath,
		"path to a dashsync.yaml describing multiple Docker hosts "+
			"(a missing file just means the single local daemon)")
}

// printWarnings prints one "Warning: ..." line per warning — used for a
// per-host discovery failure that isn't fatal to the overall command (see
// discoverDocker) without being silently dropped either.
func printWarnings(w io.Writer, warnings []error) {
	for _, warning := range warnings {
		fmt.Fprintln(w, "Warning:", warning)
	}
}

// NewRootCmd builds the root dashsync command with all its subcommands.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "dashsync",
		Short:         "Idempotent git-native sync from Docker containers to self-hosted dashboard configs",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(newVersionCmd(buildinfo.Get))
	cmd.AddCommand(newInspectCmd(discoverDocker))
	cmd.AddCommand(newSyncCmd(discoverDocker))

	return cmd
}

// discoverDocker is the real, Docker-connecting implementation wired into
// newInspectCmd and newSyncCmd — everything discovery-specific (label
// parsing, per-host client construction, concurrency) lives in
// internal/discovery, and everything about turning per-host results into
// the (services, warnings, error) shape lives in aggregateHostResults,
// which is dedicated-tested on its own. What's left here — config.Load's
// error propagating as this function's own fatal error — is covered
// directly by TestDiscoverDocker_InvalidConfigIsAFatalError, since that
// path doesn't need a real Docker daemon to exercise (it returns before
// ever reaching discovery.DiscoverAll). The connect/discover happy path
// isn't unit-tested here — that needs a real daemon, which is what
// internal/cli/integration_test.go's build-tagged e2e test is for.
//
// configPath is loaded fresh on every call rather than once at startup —
// config.Load's own "missing file" default keeps a zero-config
// `dashsync sync` behaving exactly as it did before multi-host support
// existed.
func discoverDocker(ctx context.Context, hostAddr, configPath string) ([]model.Service, []error, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dockerCallTimeout)
	defer cancel()

	return aggregateHostResults(discovery.DiscoverAll(ctx, cfg.Hosts, hostAddr))
}

// aggregateHostResults turns DiscoverAll's per-host results into the
// (services, warnings, error) shape newInspectCmd/newSyncCmd's discover
// function returns: a host that failed to connect or list its containers
// becomes a warning rather than aborting the command — the point of
// multi-host support is that one unreachable daemon doesn't stop the
// others from being synced — and every host's Services are flattened into
// one deterministically sorted slice (see discovery.SortServices's own doc
// comment for why concatenation alone doesn't already produce that order).
// Only when there were hosts to try and every single one failed is there
// nothing left to report, and that's when this returns an error instead.
//
// The len(results) > 0 guard exists only so an empty result set (which
// config.Load's own validate() never actually produces — it rejects an
// empty "hosts" list — but nothing here re-derives that guarantee) reads as
// "nothing configured," not a misleading "all 0 configured host(s) failed."
func aggregateHostResults(results []discovery.HostResult) ([]model.Service, []error, error) {
	var services []model.Service
	var warnings []error
	for _, r := range results {
		if r.Err != nil {
			// r.Err already names the host for a Discover-path failure
			// (Discover wraps with host.Name itself) but only the address
			// for a connect-path failure (NewDockerClientForHost has no
			// Host.Name to work with) — prefixing the friendly name here
			// too costs a little redundancy in the first case to guarantee
			// it's actually present in the second.
			warnings = append(warnings, fmt.Errorf("host %q: %w", r.Host, r.Err))
			continue
		}
		services = append(services, r.Services...)
	}
	if len(results) > 0 && len(warnings) == len(results) {
		return nil, warnings, fmt.Errorf("all %d configured host(s) failed, nothing to discover", len(results))
	}

	discovery.SortServices(services)
	return services, warnings, nil
}

// Run is the single entry point cmd/dashsync/main.go calls. It builds the
// command tree, wires args/stdout/stderr/context into it, and turns
// whatever Execute() returns into a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	cmd := NewRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}

	return 0
}
