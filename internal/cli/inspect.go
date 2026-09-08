package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

// newInspectCmd builds the `inspect` subcommand. discover supplies the
// discovered services; the real tree wires in a function that connects to
// Docker (see discoverDocker in root.go), tests wire in a fixed result —
// the same injection pattern newVersionCmd uses for buildinfo.Info.
func newInspectCmd(discover func(ctx context.Context, hostAddr string) ([]model.Service, error)) *cobra.Command {
	var output, hostAddr string

	cmd := &cobra.Command{
		Use:           "inspect",
		Short:         "List services discovered from the Docker daemon",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			services, err := discover(cmd.Context(), hostAddr)
			if err != nil {
				return err
			}

			switch output {
			case "table":
				return printInspectTable(cmd.OutOrStdout(), services)
			case "json":
				return json.NewEncoder(cmd.OutOrStdout()).Encode(services)
			default:
				return fmt.Errorf("unsupported --output value %q: want %q or %q", output, "table", "json")
			}
		},
	}

	cmd.Flags().StringVar(&output, "output", "table", `output format: "table" or "json"`)
	addHostAddrFlag(cmd, &hostAddr)

	return cmd
}

func printInspectTable(w io.Writer, services []model.Service) error {
	if len(services) == 0 {
		_, err := fmt.Fprintln(w, "No services found. Containers need a \"dashsync.enable=true\" label to show up here.")
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tGROUP\tSTATUS\tURL\tHOST")
	for _, s := range services {
		url := s.URL
		if url == "" {
			url = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			tableSafe(s.Name), tableSafe(s.Group), tableSafe(s.Status), tableSafe(url), tableSafe(s.Source.Host))
	}
	return tw.Flush()
}

// tableSafe neutralizes the two characters tabwriter uses as structure
// (tab as the column separator, newline as the row terminator) so a
// container name or label value that happens to contain either can't
// misalign or split a row. The values here come straight from Docker
// labels — operator-controlled, but not something dashsync should trust
// to be well-behaved.
func tableSafe(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
