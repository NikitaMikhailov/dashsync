// Package cli holds dashsync's cobra wiring: building the command tree and
// the entry point cmd/dashsync/main.go calls into.
//
// TODO(author): implement NewRootCmd() and Run(). The contract and idioms
// are in the comments below; behavior is specified by the tests in
// root_test.go.
package cli

import (
	"io"

	"github.com/spf13/cobra"
)

// NewRootCmd builds the root dashsync command with all its subcommands.
//
// Idioms worth knowing here:
//
//   - cobra.Command{Use, Short} is the minimum for readable --help. Use is
//     the literal "dashsync" that shows up both in the usage text and as the
//     command's name when looking up subcommands (cmd.Commands()[i].Name()).
//   - SilenceUsage: true — without it, any error from a subcommand's RunE
//     prints the full usage text right below the error message, which for a
//     CLI tool is usually noise rather than help.
//   - SilenceErrors: true — cobra prints the error to cmd.ErrOrStderr() by
//     default. Here Run() explicitly takes over that job, so the process has
//     exactly one place deciding what to print and what exit code to use.
//   - Subcommands are registered via cmd.AddCommand(...), each built by its
//     own constructor (newVersionCmd) rather than reading package-level
//     variables — the same "no global state" principle from CLAUDE.md.
//
// Inside NewRootCmd, call newVersionCmd(buildinfo.Get) — this is the one
// and only place the real buildinfo.Get gets wired into the command tree.
// newVersionCmd itself doesn't know its data source is
// debug.ReadBuildInfo(), only the signature func() buildinfo.Info. That's
// why version_test.go can test output formatting without depending on the
// state of internal/buildinfo at all.
func NewRootCmd() *cobra.Command {
	panic("TODO: implement per the contract above and the tests in root_test.go")
}

// Run is the single entry point cmd/dashsync/main.go calls. It decides the
// process exit code itself, so main.go reduces to
// os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr)).
//
// Contract:
//   - build the tree via NewRootCmd();
//   - cmd.SetArgs(args), cmd.SetOut(stdout), cmd.SetErr(stderr) — passing
//     these explicitly instead of letting cobra commands read
//     os.Args/os.Stdout directly. Without this, Run() can't be tested
//     without spawning a real process, and `go test` would leak output onto
//     the actual terminal;
//   - if cmd.Execute() returns an error, print it to stderr (format is up
//     to you, but root_test.go looks for the substring "unknown command"
//     for the case of a nonexistent subcommand) and return 1;
//   - otherwise return 0.
func Run(args []string, stdout, stderr io.Writer) int {
	panic("TODO: implement per the contract above and the tests in root_test.go")
}
