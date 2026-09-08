package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/NikitaMikhailov/dashsync/internal/diff"
	"github.com/NikitaMikhailov/dashsync/internal/merge"
	"github.com/NikitaMikhailov/dashsync/internal/model"
	"github.com/NikitaMikhailov/dashsync/internal/render"
	"github.com/NikitaMikhailov/dashsync/internal/render/homepage"
)

// mergeableRenderer is what sync needs from a dashboard format: rendering
// a whole document (the --output-path-less, stdout-only mode) and the
// finer-grained per-entry rendering internal/merge needs for the
// idempotent, file-writing mode. Every renderer dashsync ships is expected
// to support both — the idempotent merge is the entire reason this project
// exists, not an optional extra a future format could skip.
type mergeableRenderer interface {
	render.Renderer
	merge.EntryRenderer
}

// newSyncCmd builds the `sync` subcommand. discover supplies the
// discovered services, same injection pattern as inspect and version.
func newSyncCmd(discover func(ctx context.Context, hostAddr string) ([]model.Service, error)) *cobra.Command {
	renderers := map[string]mergeableRenderer{
		"homepage": homepage.New(),
	}

	var format, hostAddr, outputPath, conflict string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Render discovered services into a dashboard config",
		Long: "sync discovers services and renders them in the chosen --format.\n\n" +
			"Without --output-path, it just prints the result to stdout — pipe or\n" +
			"redirect it yourself. With --output-path, it idempotently merges the result\n" +
			"into that file: new services are added, changed ones are updated, ones that\n" +
			"disappeared are removed, and anything you wrote by hand is left alone.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			renderer, ok := renderers[format]
			if !ok {
				return fmt.Errorf("unsupported --format value %q: want one of %s", format, formatNames(renderers))
			}
			conflictPolicy, err := parseConflictPolicy(conflict)
			if err != nil {
				return err
			}

			services, err := discover(cmd.Context(), hostAddr)
			if err != nil {
				return err
			}
			groups := model.GroupServices(services)

			if outputPath == "" {
				out, err := renderer.Render(groups)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(out)
				return err
			}

			existing, err := readIfExists(outputPath)
			if err != nil {
				return err
			}

			merged, changes, err := merge.Merge(existing, groups, renderer, merge.Options{Conflict: conflictPolicy})
			if err != nil {
				return err
			}

			if err := diff.Print(cmd.OutOrStdout(), changes); err != nil {
				return err
			}
			if dryRun {
				return nil
			}
			return writeAtomic(outputPath, merged)
		},
	}

	cmd.Flags().StringVar(&format, "format", "homepage", "dashboard format to render")
	cmd.Flags().StringVar(&outputPath, "output-path", "",
		"file to idempotently merge the result into (default: print to stdout, no file touched)")
	cmd.Flags().StringVar(&conflict, "conflict", "preserve",
		`what to do with a managed entry hand-edited since dashsync last wrote it: "preserve" (default), "overwrite", or "fail"`)
	// A cron-triggered dashsync in CI shouldn't silently start writing
	// just because something upstream changed unexpectedly — the same
	// $CI convention GitHub Actions, GitLab CI, and most others set.
	// A human running this at a terminal almost certainly wants to write.
	cmd.Flags().BoolVar(&dryRun, "dry-run", os.Getenv("CI") != "",
		"preview changes without writing anything (defaults to true when $CI is set)")
	addHostAddrFlag(cmd, &hostAddr)

	return cmd
}

// formatNames lists a renderers map's keys, sorted, for an error message —
// map iteration order is unspecified, and an error that reads differently
// on every run is its own small usability bug.
func formatNames(renderers map[string]mergeableRenderer) []string {
	names := make([]string, 0, len(renderers))
	for name := range renderers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func parseConflictPolicy(s string) (merge.ConflictPolicy, error) {
	switch s {
	case "preserve":
		return merge.Preserve, nil
	case "overwrite":
		return merge.Overwrite, nil
	case "fail":
		return merge.Fail, nil
	default:
		return 0, fmt.Errorf("unsupported --conflict value %q: want %q, %q, or %q", s, "preserve", "overwrite", "fail")
	}
}

// readIfExists reads path, returning (nil, nil) if it doesn't exist yet —
// matching merge.Merge's own contract that a nil/empty existing document
// means "no file yet," not an error.
func readIfExists(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

// defaultFileMode is what a brand-new output file gets: readable by owner
// and group, matching an ordinary config file other processes are
// expected to read — such as the dashboard container this file is likely
// bind-mounted into — rather than os.CreateTemp's more restrictive 0600
// default, which has no reason to leak into a file meant to be read by
// something else entirely.
const defaultFileMode = 0o644

// writeAtomic writes data to path without ever leaving it in a
// half-written state if the process dies partway through: a temp file in
// the same directory (so the final rename is on one filesystem, which is
// what makes it atomic) gets written and fsynced first, then renamed over
// the real path.
//
// If path already exists, its permission bits are preserved on the new
// file — a temp file's own mode has nothing to do with what path was
// already set to, and silently tightening it out from under whatever
// reads that file (a bind-mounted dashboard container running as some
// other UID, say) would be a surprising side effect of running sync — and
// its pre-write content is copied to path+".bak" at the same permissions,
// first. One version of recovery, not a full history, but enough to undo
// a sync gone wrong without reaching for git.
func writeAtomic(path string, data []byte) error {
	mode := os.FileMode(defaultFileMode)

	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		existing, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s for backup: %w", path, err)
		}
		if err := os.WriteFile(path+".bak", existing, mode); err != nil {
			return fmt.Errorf("write backup %s.bak: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".dashsync-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Cleanup for every path except success: once the rename below
	// succeeds, tmpPath no longer exists and this Remove is a silent
	// no-op — there's nothing actionable to do with that specific error,
	// which is what it means for THIS one to be silently discarded.
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("set permissions on temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file into place: %w", err)
	}
	return nil
}
