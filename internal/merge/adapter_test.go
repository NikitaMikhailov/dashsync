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
	seq, err := merge.NewHomepageAdapter().GroupSequence(file, false)
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
	_, err := merge.NewHomepageAdapter().GroupSequence(file, false)
	if err == nil {
		t.Fatal("GroupSequence() error = nil, want an error — the root isn't a list of groups")
	}
}

func TestHomepageAdapter_FindOrCreateGroup_CreatesWhenMissing(t *testing.T) {
	t.Parallel()

	file := parseFile(t, "[]\n")
	adapter := merge.NewHomepageAdapter()
	root, err := adapter.GroupSequence(file, false)
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
	root, err := adapter.GroupSequence(file, false)
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
	root, err := adapter.GroupSequence(file, false)
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
	root, err := adapter.GroupSequence(file, false)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}

	groups := adapter.Groups(root)
	if len(groups) != 1 || groups[0].Name != "Media" {
		t.Errorf("Groups() = %+v, want only the well-formed Media entry", groups)
	}
}

// --- NamedGroupAdapter: same granularity as the homepageAdapter tests
// above, directly against the interface methods rather than through a
// full Merge() call. This is what actually caught the flow-style bug
// below during review — a Merge()-level test alone didn't isolate it.

func TestNamedGroupAdapter_GroupSequence_BootstrapsOnlyWhenNewDocument(t *testing.T) {
	t.Parallel()

	// isNewDocument=true: build a fresh "services: []" regardless of
	// whatever the placeholder happened to parse as.
	file := parseFile(t, "[]\n")
	seq, err := merge.NewNamedGroupAdapter("services").GroupSequence(file, true)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}
	if len(seq.Values) != 0 {
		t.Errorf("GroupSequence() returned %d entries, want 0 (fresh document)", len(seq.Values))
	}
	if got := file.String(); got != "services: []\n" {
		t.Errorf("document = %q, want a fresh \"services: []\" document", got)
	}
}

func TestNamedGroupAdapter_GroupSequence_RejectsAnEmptySequenceThatIsNotANewDocument(t *testing.T) {
	t.Parallel()

	// The exact ambiguity isNewDocument exists to resolve: a bare "[]" is
	// also what parseOrEmpty's placeholder looks like, but here
	// isNewDocument is false (there was real, existing content — it just
	// happens to be a degenerate Homepage-shaped file, or the wrong format
	// entirely) — this must error, not silently bootstrap over it.
	file := parseFile(t, "[]\n")
	_, err := merge.NewNamedGroupAdapter("services").GroupSequence(file, false)
	if err == nil {
		t.Fatal("GroupSequence() error = nil, want an error — an empty sequence isn't a mapping with a \"services\" key")
	}
}

func TestNamedGroupAdapter_GroupSequence_AppendsAmongForeignKeys(t *testing.T) {
	t.Parallel()

	file := parseFile(t, "title: My Dashboard\nappConfig:\n  theme: dark\n")
	seq, err := merge.NewNamedGroupAdapter("services").GroupSequence(file, false)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}
	if len(seq.Values) != 0 {
		t.Errorf("GroupSequence() returned %d entries, want 0 (freshly appended)", len(seq.Values))
	}
	want := "title: My Dashboard\nappConfig:\n  theme: dark\nservices: []\n"
	if got := file.String(); got != want {
		t.Errorf("document = %q, want %q", got, want)
	}
}

func TestNamedGroupAdapter_FindOrCreateGroup_CreatesWhenMissing(t *testing.T) {
	t.Parallel()

	file := parseFile(t, "services: []\n")
	adapter := merge.NewNamedGroupAdapter("services")
	root, err := adapter.GroupSequence(file, false)
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

func TestNamedGroupAdapter_FindOrCreateGroup_FindsExisting(t *testing.T) {
	t.Parallel()

	file := parseFile(t, "services:\n  - name: Media\n    items:\n      - name: Existing\n")
	adapter := merge.NewNamedGroupAdapter("services")
	root, err := adapter.GroupSequence(file, false)
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

func TestNamedGroupAdapter_FindOrCreateGroup_AppendsItemsFieldWhenGroupLacksIt(t *testing.T) {
	t.Parallel()

	file := parseFile(t, "services:\n  - name: Media\n")
	adapter := merge.NewNamedGroupAdapter("services")
	root, err := adapter.GroupSequence(file, false)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}

	items, err := adapter.FindOrCreateGroup(root, "Media")
	if err != nil {
		t.Fatalf("FindOrCreateGroup() error = %v, want nil", err)
	}
	if len(items.Values) != 0 {
		t.Errorf("items = %+v, want a freshly appended empty list", items.Values)
	}
	want := "services:\n  - name: Media\n    items: []\n"
	if got := file.String(); got != want {
		t.Errorf("document = %q, want %q", got, want)
	}
}

func TestNamedGroupAdapter_Groups_ExtractsNameItemsPairsInOrder(t *testing.T) {
	t.Parallel()

	file := parseFile(t, "services:\n  - name: Media\n    items: []\n  - name: Network\n    items: []\n")
	adapter := merge.NewNamedGroupAdapter("services")
	root, err := adapter.GroupSequence(file, false)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}

	groups := adapter.Groups(root)
	if len(groups) != 2 || groups[0].Name != "Media" || groups[1].Name != "Network" {
		t.Errorf("Groups() = %+v, want [Media, Network] in document order", groups)
	}
}

func TestNamedGroupAdapter_Groups_SkipsEntriesItCannotInterpret(t *testing.T) {
	t.Parallel()

	file := parseFile(t, "services:\n  - name: Media\n    items: []\n  - icon: some-icon\n  - {}\n")
	adapter := merge.NewNamedGroupAdapter("services")
	root, err := adapter.GroupSequence(file, false)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}

	groups := adapter.Groups(root)
	if len(groups) != 1 || groups[0].Name != "Media" {
		t.Errorf("Groups() = %+v, want only the well-formed Media entry", groups)
	}
}

func TestNamedGroupAdapter_FindOrCreateGroup_ConvertsFlowStyleGroupsListToBlockStyle(t *testing.T) {
	t.Parallel()

	// Regression test: a hand-written groups list in flow style can't
	// legally hold the block-style content (a marker comment, a
	// multi-line entry) about to be spliced into one of its groups' items
	// — GroupSequence must normalize it to block style once it holds any
	// real group, not just fix its column.
	file := parseFile(t, "title: My Dashboard\nservices: [{name: Media, items: []}]\n")
	adapter := merge.NewNamedGroupAdapter("services")
	root, err := adapter.GroupSequence(file, false)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}
	if root.IsFlowStyle {
		t.Error("GroupSequence()'s result is still flow-style, want it normalized to block style")
	}

	items, err := adapter.FindOrCreateGroup(root, "Media")
	if err != nil {
		t.Fatalf("FindOrCreateGroup() error = %v, want nil", err)
	}
	if len(items.Values) != 0 {
		t.Errorf("items = %+v, want the existing empty list", items.Values)
	}

	// The output must actually re-parse — the concrete symptom the
	// original bug produced was invalid YAML once block content (added
	// separately by Merge, not by GroupSequence/FindOrCreateGroup alone)
	// ended up inside still-flow-style ancestors.
	if _, err := parser.ParseBytes([]byte(file.String()), parser.ParseComments); err != nil {
		t.Errorf("document does not re-parse: %v\ndocument:\n%s", err, file.String())
	}
}

func TestNamedGroupAdapter_FindOrCreateGroup_ConvertsFlowStyleGroupEntryToBlockStyle(t *testing.T) {
	t.Parallel()

	// The same bug one level deeper: the *group entry itself* (not the
	// groups list, and not the items list) hand-written as a flow-style
	// mapping with its items field already present.
	file := parseFile(t, "services:\n  - {name: Media, items: []}\n")
	adapter := merge.NewNamedGroupAdapter("services")
	root, err := adapter.GroupSequence(file, false)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}

	if _, err := adapter.FindOrCreateGroup(root, "Media"); err != nil {
		t.Fatalf("FindOrCreateGroup() error = %v, want nil", err)
	}

	mn, ok := root.Values[0].(*ast.MappingNode)
	if !ok {
		t.Fatalf("root.Values[0] is a %T, want *ast.MappingNode", root.Values[0])
	}
	if mn.IsFlowStyle {
		t.Error("the group entry is still flow-style, want it normalized to block style")
	}
}

func TestNamedGroupAdapter_FindOrCreateGroup_ConvertsThreeFieldFlowStyleGroupEntry(t *testing.T) {
	t.Parallel()

	// Same bug, three fields instead of two: normalizeMappingFlowStyle
	// recolumns *every* field to a shared baseline, not just the two it
	// happened to be found with — two fields sharing an accidental column
	// offset could mask a bug that a third field exposes, so this is its
	// own regression test rather than trusting the two-field case to
	// generalize silently.
	file := parseFile(t, "services:\n  - {icon: some-icon, name: Media, items: []}\n")
	adapter := merge.NewNamedGroupAdapter("services")
	root, err := adapter.GroupSequence(file, false)
	if err != nil {
		t.Fatalf("GroupSequence() error = %v, want nil", err)
	}

	if _, err := adapter.FindOrCreateGroup(root, "Media"); err != nil {
		t.Fatalf("FindOrCreateGroup() error = %v, want nil", err)
	}

	mn, ok := root.Values[0].(*ast.MappingNode)
	if !ok {
		t.Fatalf("root.Values[0] is a %T, want *ast.MappingNode", root.Values[0])
	}
	if mn.IsFlowStyle {
		t.Error("the group entry is still flow-style, want it normalized to block style")
	}
	if _, err := parser.ParseBytes([]byte(file.String()), parser.ParseComments); err != nil {
		t.Errorf("document does not re-parse: %v\ndocument:\n%s", err, file.String())
	}
}
