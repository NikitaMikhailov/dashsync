// Package diff formats internal/merge's []Change as human-readable text —
// what `sync` shows before writing (or instead of writing, under
// --dry-run) so a change to a shared config file is never a surprise.
package diff

import (
	"cmp"
	"fmt"
	"io"
	"slices"

	"github.com/NikitaMikhailov/dashsync/internal/merge"
)

// Print writes one line per change to w — sorted by (Group, ServiceName)
// for readability, since merge.Merge's own return order reflects its
// internal processing (all removals last, regardless of group) rather
// than anything a human would want to read top to bottom — followed by a
// one-line summary.
//
// Writing "no changes" for an empty slice is deliberate: a sync run doing
// nothing is the idempotency guarantee working exactly as intended, not
// an absence of output that needs no explanation.
func Print(w io.Writer, changes []merge.Change) error {
	if len(changes) == 0 {
		_, err := fmt.Fprintln(w, "no changes")
		return err
	}

	sorted := slices.Clone(changes)
	slices.SortFunc(sorted, func(a, b merge.Change) int {
		if c := cmp.Compare(a.Group, b.Group); c != 0 {
			return c
		}
		if c := cmp.Compare(a.ServiceName, b.ServiceName); c != 0 {
			return c
		}
		return cmp.Compare(a.ServiceID, b.ServiceID)
	})

	counts := make(map[merge.ChangeKind]int, 4)
	for _, c := range sorted {
		counts[c.Kind]++
		if _, err := fmt.Fprintln(w, formatLine(c)); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintf(w, "%d change(s): %d added, %d updated, %d removed, %d conflict(s)\n",
		len(sorted), counts[merge.Added], counts[merge.Updated], counts[merge.Removed], counts[merge.Conflict])
	return err
}

func formatLine(c merge.Change) string {
	name := c.ServiceName
	if name == "" {
		// Removed changes only carry the old marker's ID — the service is
		// already gone from the desired set by the time Merge notices it,
		// so there's no display name left to report.
		name = c.ServiceID
	}

	line := fmt.Sprintf("%s %s: %s (%s)", symbol(c.Kind), c.Group, name, c.Kind)
	if c.Kind == merge.Conflict {
		line += " — hand-edited since dashsync last wrote it; left as-is (use --conflict=overwrite to replace it anyway)"
	}
	return line
}

// symbol is the one-character prefix each change kind gets, the same
// convention `git status`/`git diff --stat` use.
func symbol(k merge.ChangeKind) string {
	switch k {
	case merge.Added:
		return "+"
	case merge.Updated:
		return "~"
	case merge.Removed:
		return "-"
	case merge.Conflict:
		return "!"
	default:
		return "?"
	}
}
