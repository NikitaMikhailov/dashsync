package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/NikitaMikhailov/dashsync/internal/buildinfo"
)

// newVersionCmd builds the `version` subcommand. info supplies the build
// metadata to print; the real tree wires in buildinfo.Get, tests wire in a
// fixed value.
func newVersionCmd(info func() buildinfo.Info) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:           "version",
		Short:         "Print the dashsync version",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch output {
			case "text":
				return printVersionText(cmd.OutOrStdout(), info())
			case "json":
				return json.NewEncoder(cmd.OutOrStdout()).Encode(info())
			default:
				return fmt.Errorf("unsupported --output value %q: want %q or %q", output, "text", "json")
			}
		},
	}

	cmd.Flags().StringVar(&output, "output", "text", `output format: "text" or "json"`)

	return cmd
}

func printVersionText(w io.Writer, info buildinfo.Info) error {
	commit := info.Commit
	if commit == "" {
		commit = "unknown"
	}
	built := info.Date
	if built == "" {
		built = "unknown"
	}

	_, err := fmt.Fprintf(w, "dashsync %s\ncommit:  %s\nbuilt:   %s\n", info.Version, commit, built)
	return err
}
