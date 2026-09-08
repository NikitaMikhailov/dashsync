package diff_test

import (
	"bytes"
	"testing"

	"github.com/NikitaMikhailov/dashsync/internal/diff"
	"github.com/NikitaMikhailov/dashsync/internal/merge"
)

func TestPrint_NoChanges(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := diff.Print(&buf, nil); err != nil {
		t.Fatalf("Print() error = %v, want nil", err)
	}
	if got := buf.String(); got != "no changes\n" {
		t.Errorf("Print() = %q, want %q", got, "no changes\n")
	}
}

func TestPrint_SortsByGroupThenName(t *testing.T) {
	t.Parallel()

	// Deliberately out of order and out of group order — Print must sort,
	// not just echo Merge's own (removal-last) processing order.
	changes := []merge.Change{
		{Kind: merge.Removed, ServiceID: "old1", Group: "Network"},
		{Kind: merge.Added, ServiceID: "id2", ServiceName: "Sonarr", Group: "Media"},
		{Kind: merge.Added, ServiceID: "id1", ServiceName: "AdGuard", Group: "Network"},
		{Kind: merge.Updated, ServiceID: "id3", ServiceName: "Jellyfin", Group: "Media"},
	}

	var buf bytes.Buffer
	if err := diff.Print(&buf, changes); err != nil {
		t.Fatalf("Print() error = %v, want nil", err)
	}

	// Within Network, the Removed change (empty ServiceName, since Merge
	// only has its old ID by the time it's noticed) sorts before AdGuard —
	// plain string comparison, "" < "AdGuard", nothing special-cased.
	want := `~ Media: Jellyfin (updated)
+ Media: Sonarr (added)
- Network: old1 (removed)
+ Network: AdGuard (added)
4 change(s): 2 added, 1 updated, 1 removed, 0 conflict(s)
`
	if got := buf.String(); got != want {
		t.Errorf("Print() =\n%s\nwant:\n%s", got, want)
	}
}

func TestPrint_ConflictExplainsItself(t *testing.T) {
	t.Parallel()

	changes := []merge.Change{
		{Kind: merge.Conflict, ServiceID: "id1", ServiceName: "Jellyfin", Group: "Media"},
	}

	var buf bytes.Buffer
	if err := diff.Print(&buf, changes); err != nil {
		t.Fatalf("Print() error = %v, want nil", err)
	}

	got := buf.String()
	if !containsAll(got, "! Media: Jellyfin (conflict)", "hand-edited", "--conflict=overwrite") {
		t.Errorf("Print() = %q, want it to explain the conflict and how to resolve it", got)
	}
}

func TestPrint_RemovedChangeHasNoName(t *testing.T) {
	t.Parallel()

	// Merge only has the old marker's ID for a Removed change — the
	// service is already gone from the desired set by the time it
	// notices. Print must still produce something readable, not a blank.
	changes := []merge.Change{{Kind: merge.Removed, ServiceID: "abc123", Group: "Media"}}

	var buf bytes.Buffer
	if err := diff.Print(&buf, changes); err != nil {
		t.Fatalf("Print() error = %v, want nil", err)
	}
	if !containsAll(buf.String(), "- Media: abc123 (removed)") {
		t.Errorf("Print() = %q, want the ID used in place of a name", buf.String())
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !bytes.Contains([]byte(s), []byte(sub)) {
			return false
		}
	}
	return true
}
