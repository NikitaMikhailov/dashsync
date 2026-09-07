package cli

import (
	"github.com/spf13/cobra"

	"github.com/NikitaMikhailov/dashsync/internal/buildinfo"
)

// newVersionCmd builds the `version` subcommand.
//
// TODO(author): implement. The contract and idioms are below; the exact
// output format is specified by the tests in version_test.go.
//
// The info parameter is a function that returns build metadata, not the
// buildinfo package itself: in the real command tree (see NewRootCmd) this
// is buildinfo.Get, while tests pass a stub returning a fixed value. That
// way version_test.go only exercises formatting and flag parsing, without
// depending on whether internal/buildinfo is implemented yet — "accept
// interfaces, return structs" in practice, just with a one-function
// interface here instead of a whole package.
//
// The --output flag accepts "text" (the default) and "json"; any other
// value is an error, not a silent fallback to text.
//
// Format contract (exact strings live in version_test.go):
//
//   - text: exactly three lines, in this order —
//     "dashsync {Version}\n"
//     "commit:  {Commit, or \"unknown\" if empty}\n"
//     "built:   {Date, or \"unknown\" if empty}\n"
//   - json: encoding/json of info()'s return value; the version/commit/date
//     tags are already set on buildinfo.Info — see
//     internal/buildinfo/buildinfo.go.
//   - an unrecognized --output value: return an error whose text mentions
//     the value itself (the test looks for it as a substring) — so the user
//     sees exactly what they got wrong instead of just "bad flag".
//
// Don't forget cmd.SilenceUsage = true on this command too: version_test.go
// builds it standalone, without a parent, and in that shape cobra prints
// usage under every error by default.
func newVersionCmd(info func() buildinfo.Info) *cobra.Command {
	panic("TODO: implement per the contract above and the tests in version_test.go")
}
