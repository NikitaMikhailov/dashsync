package cli

import (
	"context"
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/NikitaMikhailov/dashsync/internal/model"
	"github.com/NikitaMikhailov/dashsync/internal/render"
	"github.com/NikitaMikhailov/dashsync/internal/render/homepage"
)

// newSyncCmd builds the `sync` subcommand. discover supplies the discovered
// services, same injection pattern as inspect and version.
//
// sync only prints to stdout for now — writing the result into an existing
// file without clobbering hand-edited entries needs the idempotent merge
// that lands in M3. Until then, redirecting stdout is the way to get the
// output onto disk.
func newSyncCmd(discover func(ctx context.Context, hostAddr string) ([]model.Service, error)) *cobra.Command {
	renderers := map[string]render.Renderer{
		"homepage": homepage.New(),
	}

	var format, hostAddr string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Render discovered services into a dashboard config",
		Long: "sync discovers services and renders them in the chosen --format, printing the\n" +
			"result to stdout. Writing it into an existing file without touching hand-edited\n" +
			"entries is M3's idempotent merge; for now, redirect stdout yourself.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			renderer, ok := renderers[format]
			if !ok {
				return fmt.Errorf("unsupported --format value %q: want one of %s", format, formatNames(renderers))
			}

			services, err := discover(cmd.Context(), hostAddr)
			if err != nil {
				return err
			}

			out, err := renderer.Render(model.GroupServices(services))
			if err != nil {
				return err
			}

			_, err = cmd.OutOrStdout().Write(out)
			return err
		},
	}

	cmd.Flags().StringVar(&format, "format", "homepage", "dashboard format to render")
	addHostAddrFlag(cmd, &hostAddr)

	return cmd
}

// formatNames lists a renderers map's keys, sorted, for an error message —
// map iteration order is unspecified, and an error that reads differently
// on every run is its own small usability bug.
func formatNames(renderers map[string]render.Renderer) []string {
	names := make([]string, 0, len(renderers))
	for name := range renderers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
