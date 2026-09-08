package merge

// Spike tests for getOrAppendField, written and run before the rest of
// NamedGroupAdapter is built on top of it — the same "verify against real
// library behavior, don't just reason about it" discipline that caught the
// flow-style-column and hand-edit-detection bugs during this package's
// original development (see ADR 002). Package merge (not merge_test): these
// exercise an unexported helper directly, at the AST level, rather than
// through the whole Merge() pipeline.

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

func TestGetOrAppendField_AppendsAtRootAmongForeignKeys(t *testing.T) {
	t.Parallel()

	// The real case this exists for: a Homer/Dashy-shaped document where
	// "services:"/"sections:" doesn't exist yet, alongside unrelated
	// hand-configured settings that must survive byte-for-byte.
	src := "title: My Dashboard\nappConfig:\n  theme: dark\n"
	f, err := parser.ParseBytes([]byte(src), parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	mn, ok := f.Docs[0].Body.(*ast.MappingNode)
	if !ok {
		t.Fatalf("root is a %T, want *ast.MappingNode", f.Docs[0].Body)
	}

	field, err := getOrAppendField(mn, "services")
	if err != nil {
		t.Fatalf("getOrAppendField() error = %v, want nil", err)
	}
	if _, ok := field.Value.(*ast.SequenceNode); !ok {
		t.Fatalf("field.Value is a %T, want *ast.SequenceNode", field.Value)
	}

	got := f.String()
	want := "title: My Dashboard\nappConfig:\n  theme: dark\nservices: []\n"
	if got != want {
		t.Errorf("document =\n%q\nwant:\n%q", got, want)
	}
}

func TestGetOrAppendField_FindsExistingFieldWithoutDuplicating(t *testing.T) {
	t.Parallel()

	src := "title: My Dashboard\nservices:\n  - name: Media\n    items: []\n"
	f, err := parser.ParseBytes([]byte(src), parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	mn, ok := f.Docs[0].Body.(*ast.MappingNode)
	if !ok {
		t.Fatalf("root is a %T, want *ast.MappingNode", f.Docs[0].Body)
	}

	field, err := getOrAppendField(mn, "services")
	if err != nil {
		t.Fatalf("getOrAppendField() error = %v, want nil", err)
	}
	seq, ok := field.Value.(*ast.SequenceNode)
	if !ok || len(seq.Values) != 1 {
		t.Fatalf("field.Value = %+v, want the existing one-entry sequence, unchanged", field.Value)
	}
	if len(mn.Values) != 2 {
		t.Errorf("mn now has %d top-level fields, want still 2 (title, services) — no duplicate appended", len(mn.Values))
	}

	got := f.String()
	if got != src {
		t.Errorf("document changed when the field already existed:\ngot:\n%q\nwant (unchanged):\n%q", got, src)
	}
}

func TestGetOrAppendField_AppendsItemsFieldOnAGroupMissingIt(t *testing.T) {
	t.Parallel()

	// A hand-written group entry with no "items:" key at all yet —
	// getOrAppendField must work one level deeper than the document root
	// too, since a group entry is itself a *ast.MappingNode.
	src := "- name: Media\n  icon: some-icon\n"
	f, err := parser.ParseBytes([]byte(src), parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	seq, ok := f.Docs[0].Body.(*ast.SequenceNode)
	if !ok || len(seq.Values) != 1 {
		t.Fatalf("root is a %T with %d entries, want a 1-entry *ast.SequenceNode", f.Docs[0].Body, len(seq.Values))
	}
	mn, ok := seq.Values[0].(*ast.MappingNode)
	if !ok {
		t.Fatalf("group entry is a %T, want *ast.MappingNode", seq.Values[0])
	}

	field, err := getOrAppendField(mn, "items")
	if err != nil {
		t.Fatalf("getOrAppendField() error = %v, want nil", err)
	}
	if _, ok := field.Value.(*ast.SequenceNode); !ok {
		t.Fatalf("field.Value is a %T, want *ast.SequenceNode", field.Value)
	}

	got := f.String()
	want := "- name: Media\n  icon: some-icon\n  items: []\n"
	if got != want {
		t.Errorf("document =\n%q\nwant:\n%q", got, want)
	}
}

func TestGetOrAppendField_AppendsAtRootWithNoExistingFields(t *testing.T) {
	t.Parallel()

	// The bootstrap case: an entirely fresh {} mapping (no foreign keys to
	// preserve, nothing to compute a target column from).
	f, err := parser.ParseBytes([]byte("{}\n"), parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	mn, ok := f.Docs[0].Body.(*ast.MappingNode)
	if !ok {
		t.Fatalf("root is a %T, want *ast.MappingNode", f.Docs[0].Body)
	}

	if _, err := getOrAppendField(mn, "sections"); err != nil {
		t.Fatalf("getOrAppendField() error = %v, want nil", err)
	}

	got := f.String()
	want := "sections: []\n"
	if got != want {
		t.Errorf("document = %q, want %q", got, want)
	}
}
