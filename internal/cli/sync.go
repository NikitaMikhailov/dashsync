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
	"github.com/NikitaMikhailov/dashsync/internal/render/dashy"
	"github.com/NikitaMikhailov/dashsync/internal/render/homepage"
	"github.com/NikitaMikhailov/dashsync/internal/render/homer"
)

// mergeableRenderer is additionally implemented by a render.Renderer that
// supports internal/merge's idempotent, file-writing mode: it needs the
// finer-grained per-entry rendering (and, implicitly, a document shape
// Merge knows how to walk via merge.DocumentAdapter) that plain
// whole-document Render doesn't provide. Homepage, Homer, and Dashy all
// implement this today — see docs/decisions/007-document-adapter.md for
// how Homer and Dashy got there — but the assertion below stays: nothing
// guarantees a future renderer's document shape fits an existing
// DocumentAdapter either, and --output-path should fail clearly for one
// that doesn't rather than a bad merge or a panic.
type mergeableRenderer interface {
	render.Renderer
	merge.EntryRenderer
}

// newSyncCmd builds the `sync` subcommand. discover supplies the
// discovered services and any non-fatal per-host warnings, same injection
// pattern as inspect and version.
func newSyncCmd(discover func(ctx context.Context, hostAddr, configPath string) ([]model.Service, []error, error)) *cobra.Command {
	renderers := map[string]render.Renderer{
		"homepage": homepage.New(),
		"homer":    homer.New(),
		"dashy":    dashy.New(),
	}

	var format, hostAddr, configPath, outputPath, conflict string
	var dryRun, check bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Render discovered services into a dashboard config",
		Long: "sync discovers services and renders them in the chosen --format.\n\n" +
			"Without --output-path, it just prints the result to stdout — pipe or\n" +
			"redirect it yourself. With --output-path, it idempotently merges the result\n" +
			"into that file: new services are added, changed ones are updated, ones that\n" +
			"disappeared are removed, and anything you wrote by hand is left alone.\n\n" +
			"A future --format that doesn't support this yet will say so and exit\n" +
			"before touching anything.\n\n" +
			"--check never writes either, like --dry-run, but exits 2 if the file has\n" +
			"pending changes — distinct from exit 1 for any other failure, so a CI\n" +
			"job can tell 'drifted from Docker's current state' apart from 'broke.'",
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
			if check && outputPath == "" {
				return errors.New("--check has nothing to compare against without --output-path")
			}

			if outputPath == "" {
				services, warnings, err := discover(cmd.Context(), hostAddr, configPath)
				printWarnings(cmd.ErrOrStderr(), warnings)
				if err != nil {
					return err
				}
				out, err := renderer.Render(model.GroupServices(services))
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(out)
				return err
			}

			// Checked before discover() runs: format and --output-path are
			// both known already, so a mismatch between them is reported
			// without first paying for a Docker round-trip that has
			// nothing to do with the actual problem.
			mr, ok := renderer.(mergeableRenderer)
			if !ok {
				return fmt.Errorf(
					"--format %q doesn't support --output-path yet (no idempotent merge implemented for it) — "+
						"omit --output-path to print to stdout instead", format)
			}

			services, warnings, err := discover(cmd.Context(), hostAddr, configPath)
			printWarnings(cmd.ErrOrStderr(), warnings)
			if err != nil {
				return err
			}
			groups := model.GroupServices(services)

			// Only a run that's actually going to write needs to keep
			// other writers out; --check and --dry-run are both read-only
			// previews that can safely share the lock with each other (or
			// with nothing at all) — see acquireLock's own doc comment.
			release, err := acquireLock(outputPath, !check && !dryRun)
			if err != nil {
				return err
			}
			defer release() //nolint:errcheck // releasing a lock we're about to exit the process under has nothing useful to do with a failure

			existing, err := readIfExists(outputPath)
			if err != nil {
				return err
			}

			merged, changes, err := merge.Merge(existing, groups, mr, merge.Options{Conflict: conflictPolicy})
			if err != nil {
				return err
			}

			if err := diff.Print(cmd.OutOrStdout(), changes); err != nil {
				return err
			}
			if check {
				if len(changes) > 0 {
					return &pendingChangesError{count: len(changes), path: outputPath}
				}
				return nil
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
	cmd.Flags().BoolVar(&check, "check", false,
		"like --dry-run, but exit 2 if the file has pending changes, distinct from exit 1 for any other "+
			"failure (requires --output-path); for a job that should fail when the file has drifted from Docker's current state")
	addHostAddrFlag(cmd, &hostAddr)
	addConfigFlag(cmd, &configPath)

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

// pendingChangesError is --check's own signal that internal/cli.Run maps
// to exitPendingChanges instead of the generic exit 1 every other sync
// failure gets — see that constant's doc comment for why the distinction
// matters. The message doesn't reconstruct a full command line — this
// same sync invocation could carry --format, --conflict, or other flags
// that matter for reproducing the identical merge, and hardcoding a bare
// "dashsync sync --output-path X" would tell the reader to run something
// different from what they actually ran. It names --dry-run=false and
// --check explicitly instead of vaguely "run without --check": --check's
// audience is a CI job, which is exactly where $CI-triggered --dry-run
// defaults to true, so dropping --check alone would still silently write
// nothing in the environment this message is most likely read in.
type pendingChangesError struct {
	count int
	path  string
}

func (e *pendingChangesError) Error() string {
	return fmt.Sprintf(
		"%d pending change(s) not yet applied to %s — rerun this same sync command with --dry-run=false and without --check to write them",
		e.count, e.path)
}
