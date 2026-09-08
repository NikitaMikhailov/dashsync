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
	"github.com/NikitaMikhailov/dashsync/internal/discovery"
	"github.com/NikitaMikhailov/dashsync/internal/model"
)

// dockerCallTimeout bounds how long a single Docker API call is allowed to
// take. Without it, a stale socket or a hung daemon leaves `inspect`
// blocked forever with no feedback — an infrastructure CLI shouldn't have
// an unbounded wait against an external dependency as its only failure
// mode.
const dockerCallTimeout = 10 * time.Second

// defaultHostAddr is the fallback for --host-addr on every command that
// takes it (inspect, sync): the host or IP used to build a URL
// auto-detected from a container's published ports.
const defaultHostAddr = "localhost"

// addHostAddrFlag registers --host-addr identically on every command that
// needs it. Defined once so inspect and sync can't quietly drift apart on
// the flag's default or help text.
func addHostAddrFlag(cmd *cobra.Command, hostAddr *string) {
	cmd.Flags().StringVar(hostAddr, "host-addr", defaultHostAddr,
		"host or IP used to build URLs auto-detected from published ports")
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
// newInspectCmd — everything discovery-specific (label parsing, sorting)
// lives in internal/discovery; this is just the connect/close boilerplate
// and timeout policy around it, which is why it's not itself covered by a
// dedicated test: there's no branch here that isn't either "delegates to
// something already tested" or "wiring that go build already verifies."
func discoverDocker(ctx context.Context, hostAddr string) ([]model.Service, error) {
	dockerClient, err := discovery.NewDockerClient()
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // closing a client we're about to discard: nothing
	// actionable to do with a Close error here, and nothing downstream
	// depends on it succeeding.
	defer dockerClient.Close()

	ctx, cancel := context.WithTimeout(ctx, dockerCallTimeout)
	defer cancel()

	return discovery.Discover(ctx, discovery.Host{Name: "local", Client: dockerClient}, hostAddr)
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
