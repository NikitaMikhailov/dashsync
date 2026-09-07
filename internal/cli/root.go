// Package cli holds dashsync's cobra wiring: building the command tree and
// the entry point cmd/dashsync/main.go calls into.
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/NikitaMikhailov/dashsync/internal/buildinfo"
)

// NewRootCmd builds the root dashsync command with all its subcommands.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "dashsync",
		Short:         "Idempotent git-native sync from Docker containers to self-hosted dashboard configs",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(newVersionCmd(buildinfo.Get))

	return cmd
}

// Run is the single entry point cmd/dashsync/main.go calls. It builds the
// command tree, wires args/stdout/stderr into it, and turns whatever
// Execute() returns into a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	cmd := NewRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}

	return 0
}
