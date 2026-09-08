package merge_test

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"

	"github.com/NikitaMikhailov/dashsync/internal/merge"
)

// parseFile parses src as a dashsync-managed document, failing the test on
// any parse error — the same shape merge.Merge's own parseOrEmpty produces,
// but built directly here so these tests can inspect a DocumentAdapter's
// return values on their own, without going through the whole Merge
// pipeline that internal/merge's other tests already exercise.
func parseFile(t *testing.T, src string) *ast.File {
	t.Helper()
	f, err := parser.ParseBytes([]byte(src), parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return f
}

func TestHomepageAdapter_GroupSequence_ReturnsRootSequence(t *testing.T) {
	t.Parallel()

	file := parseFile(t, "- Media: []\n")
	seq, err := merge.NewHomepageAdapter().GroupSequence(file)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}
	if len(seq.Values) != 1 {
		t.Errorf("GroupSequence() returned %d entries, want 1 (the document's own root)", len(seq.Values))
	}
}

func TestHomepageAdapter_GroupSequence_RejectsNonSequenceRoot(t *testing.T) {
	t.Parallel()

	file := parseFile(t, "not_a_list: true\n")
	_, err := merge.NewHomepageAdapter().GroupSequence(file)
	if err == nil {
		t.Fatal("GroupSequence() error = nil, want an error — the root isn't a list of groups")
	}
}

func TestHomepageAdapter_FindOrCreateGroup_CreatesWhenMissing(t *testing.T) {
	t.Parallel()

	file := parseFile(t, "[]\n")
	adapter := merge.NewHomepageAdapter()
	root, err := adapter.GroupSequence(file)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}

	items, err := adapter.FindOrCreateGroup(root, "Media")
	if err != nil {
		t.Fatalf("FindOrCreateGroup() error = %v, want nil", err)
	}
	if len(items.Values) != 0 {
		t.Errorf("FindOrCreateGroup() new group has %d items, want 0", len(items.Values))
	}
	if len(root.Values) != 1 {
		t.Errorf("root now has %d entries, want 1 (the newly created group)", len(root.Values))
	}
}

func TestHomepageAdapter_FindOrCreateGroup_FindsExisting(t *testing.T) {
	t.Parallel()

	file := parseFile(t, "- Media:\n    - Existing:\n        href: http://x\n")
	adapter := merge.NewHomepageAdapter()
	root, err := adapter.GroupSequence(file)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}

	items, err := adapter.FindOrCreateGroup(root, "Media")
	if err != nil {
		t.Fatalf("FindOrCreateGroup() error = %v, want nil", err)
	}
	if len(items.Values) != 1 {
		t.Fatalf("FindOrCreateGroup() found %d items, want the 1 already in the file", len(items.Values))
	}
	if len(root.Values) != 1 {
		t.Errorf("root now has %d entries, want still 1 — finding an existing group must not create a duplicate", len(root.Values))
	}
}

func TestHomepageAdapter_Groups_ExtractsNameItemsPairsInOrder(t *testing.T) {
	t.Parallel()

	file := parseFile(t, "- Media: []\n- Network: []\n")
	adapter := merge.NewHomepageAdapter()
	root, err := adapter.GroupSequence(file)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}

	groups := adapter.Groups(root)
	if len(groups) != 2 || groups[0].Name != "Media" || groups[1].Name != "Network" {
		t.Errorf("Groups() = %+v, want [Media, Network] in document order", groups)
	}
}

func TestHomepageAdapter_Groups_SkipsEntriesItCannotInterpret(t *testing.T) {
	t.Parallel()

	// A scalar entry and an empty mapping are both malformed as a
	// "Name: [...]" group — Groups must skip them silently (they're
	// untouched hand-written content, not dashsync's to manage) rather
	// than erroring the whole removal pass over content it doesn't own.
	file := parseFile(t, "- Media: []\n- just a string, not a group\n- {}\n")
	adapter := merge.NewHomepageAdapter()
	root, err := adapter.GroupSequence(file)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}

	groups := adapter.Groups(root)
	if len(groups) != 1 || groups[0].Name != "Media" {
		t.Errorf("Groups() = %+v, want only the well-formed Media entry", groups)
	}
}
