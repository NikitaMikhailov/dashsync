package merge

import (
	"fmt"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// DocumentAdapter is the document-shape-specific half of merge support: it
// knows how to locate — and, on a first write, create — the sequence of
// group entries within a specific dashboard format's document, and how to
// find or create a named group's own items sequence within that list.
//
// Everything else in this package that operates on group/item content once
// GroupSequence/FindOrCreateGroup have located it (marker parsing,
// hand-edit detection, the empty-sequence trick, the removal pass) works
// purely on *ast.SequenceNode and has no need to know which DocumentAdapter
// produced it — see readEntries/writeEntries/applyDesired/removeOrphaned.
// Entry identity and hand-edit detection are entirely marker-comment-based,
// never content-based, which is what makes that possible.
//
// This is verified against two shapes: homepageAdapter (Homepage's own,
// below) and NamedGroupAdapter (Homer/Dashy's shared shape, also below) —
// see docs/decisions/007-document-adapter.md.
type DocumentAdapter interface {
	// GroupSequence returns the document's list of group entries.
	// isNewDocument is true when Merge was called with no existing content
	// at all (nil or blank bytes) — computed from the raw input, before
	// parseOrEmpty's own "no file yet" placeholder (an empty
	// *ast.SequenceNode) gets parsed into file, because that placeholder
	// is indistinguishable, by shape alone, from a real Homer/Dashy file
	// that happens to be malformed or pointed at the wrong format (e.g. an
	// actual Homepage services.yaml, whose own valid "no groups" shape is
	// also an empty sequence). An adapter whose shape doesn't share
	// Homepage's own root-is-the-sequence structure needs this to bootstrap
	// a fresh document only when the file genuinely didn't exist, not
	// whenever it happens to parse to something shaped like its own empty
	// placeholder would be.
	GroupSequence(file *ast.File, isNewDocument bool) (*ast.SequenceNode, error)

	// FindOrCreateGroup returns the items sequence for the group entry in
	// groups identified as name, creating a new empty group entry
	// (appended at the end of groups) if none matches.
	FindOrCreateGroup(groups *ast.SequenceNode, name string) (*ast.SequenceNode, error)

	// Groups returns every group entry already in groups as (name, items)
	// pairs, in document order, silently skipping any entry this adapter
	// can't interpret as a well-formed group (untouched hand-written
	// content) rather than erroring — Merge's removal pass needs to sweep
	// every group currently in the file, including ones this run's
	// desired set says nothing about, without choking on ones it can't
	// parse. Verified for homepageAdapter's shape below; not yet checked
	// against a shape where "not a well-formed group" can mean something
	// more varied than "not a single-key map."
	Groups(groups *ast.SequenceNode) []NamedGroup
}

// NamedGroup is one (name, items) pair as found by DocumentAdapter.Groups.
type NamedGroup struct {
	Name  string
	Items *ast.SequenceNode
}

// homepageAdapter implements DocumentAdapter for Homepage's shape: the
// document's own root is the group sequence, and a group is a single-key
// map (the key is the name, the value is the items sequence) — exactly
// what internal/merge was originally built against, before DocumentAdapter
// existed to let a second shape coexist with it. GroupSequence and
// FindOrCreateGroup delegate to ast.go's rootSequence/
// findOrCreateGroupSequence (unchanged from before this type existed);
// Groups' own body is this package's former inline removal-pass loop,
// moved here rather than delegated to an ast.go helper.
type homepageAdapter struct{}

// NewHomepageAdapter returns the DocumentAdapter for Homepage's document
// shape.
func NewHomepageAdapter() DocumentAdapter { return homepageAdapter{} }

// GroupSequence implements DocumentAdapter. isNewDocument is unused:
// Homepage's shape has no ambiguity to resolve with it — a file that
// parses to an empty sequence root always legitimately means "zero
// groups," whether or not it "really" existed on disk, since root itself
// is the groups list either way.
func (homepageAdapter) GroupSequence(file *ast.File, _ bool) (*ast.SequenceNode, error) {
	seq, err := rootSequence(file)
	if err != nil {
		return nil, err
	}
	normalizeGroupsListStyle(seq)
	return seq, nil
}

func (homepageAdapter) FindOrCreateGroup(groups *ast.SequenceNode, name string) (*ast.SequenceNode, error) {
	return findOrCreateGroupSequence(groups, name)
}

func (homepageAdapter) Groups(groups *ast.SequenceNode) []NamedGroup {
	out := make([]NamedGroup, 0, len(groups.Values))
	for _, v := range groups.Values {
		mn, ok := v.(*ast.MappingNode)
		if !ok || len(mn.Values) == 0 {
			continue
		}
		name, ok := nodeKeyString(mn.Values[0].Key)
		if !ok {
			continue
		}
		seq, ok := mn.Values[0].Value.(*ast.SequenceNode)
		if !ok {
			continue
		}
		out = append(out, NamedGroup{Name: name, Items: seq})
	}
	return out
}

// namedGroup marshals to a fresh "name: <name>\nitems: []\n" group entry —
// the shape NamedGroupAdapter.FindOrCreateGroup builds when no existing
// entry matches. Going through yaml.Marshal rather than hand-built YAML
// text means a name needing quoting comes out correctly quoted, the same
// reason parseGroupNode does for Homepage.
type namedGroup struct {
	Name  string     `yaml:"name"`
	Items []struct{} `yaml:"items"`
}

// NamedGroupAdapter implements DocumentAdapter for a document shaped as:
//
//	<topLevelKey>:
//	  - name: <group>
//	    items: [...]
//
// nested inside an otherwise-foreign document (hand-configured settings
// alongside it) — Homer's "services:" and Dashy's "sections:" are the two
// known instances of this shape (see docs/decisions/007-document-adapter.md).
// Group identity ("name") and the items field name ("items") are hardcoded,
// not parameterized: both real formats agree on them, and parameterizing
// an axis with only one known answer is exactly the premature-abstraction
// problem docs/decisions/003-homer-render-only.md already warned against.
// If a third format disagrees on either, that's the moment to add a
// parameter for it — not before.
type NamedGroupAdapter struct {
	topLevelKey string
}

// NewNamedGroupAdapter returns the DocumentAdapter for a document whose
// managed content lives under topLevelKey (e.g. "services" for Homer,
// "sections" for Dashy).
func NewNamedGroupAdapter(topLevelKey string) DocumentAdapter {
	return NamedGroupAdapter{topLevelKey: topLevelKey}
}

// GroupSequence implements DocumentAdapter. Unlike homepageAdapter, this
// has real bootstrap work to do: parseOrEmpty's "no file yet" placeholder
// is a bare empty sequence, which is Homepage's own shape, not this one —
// a real Homer/Dashy document's root is always a mapping (pageInfo,
// appConfig, title, ...). isNewDocument, not the parsed shape, is what
// decides whether to replace it with a minimal fresh
// "<topLevelKey>: []" document: relying on shape alone (an empty sequence
// root) would also match a genuinely existing file that's the wrong format
// entirely (an actual Homepage services.yaml with zero groups, say) and
// silently overwrite it instead of reporting the mismatch.
func (a NamedGroupAdapter) GroupSequence(file *ast.File, isNewDocument bool) (*ast.SequenceNode, error) {
	if isNewDocument {
		fresh, err := parser.ParseBytes([]byte(a.topLevelKey+": []\n"), parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("build empty %s document: %w", a.topLevelKey, err)
		}
		file.Docs[0].Body = fresh.Docs[0].Body
	}

	mn, ok := file.Docs[0].Body.(*ast.MappingNode)
	if !ok {
		return nil, fmt.Errorf("existing config's top level is a %s, not a mapping with a %q key — dashsync can't merge into it",
			file.Docs[0].Body.Type(), a.topLevelKey)
	}

	field, err := getOrAppendField(mn, a.topLevelKey)
	if err != nil {
		return nil, fmt.Errorf("find or create %q: %w", a.topLevelKey, err)
	}
	seq, ok := field.Value.(*ast.SequenceNode)
	if !ok {
		return nil, fmt.Errorf("%q is not a list of groups — dashsync can't merge into it", a.topLevelKey)
	}
	fixFlowStyleColumn(seq, field.Key.GetToken().Position.Column)
	normalizeGroupsListStyle(seq)
	return seq, nil
}

// FindOrCreateGroup implements DocumentAdapter.
//
// Known limitation, shared with homepageAdapter's own findOrCreateGroupSequence
// (not new here): two hand-written group entries with the same name pick
// the first match silently — the second becomes inert, un-managed content
// forever. Not detected or rejected; a real-world hand-edit producing a
// duplicate name is rare enough, and validating uniqueness across every
// existing entry on every call would need to run whether or not this run
// actually touches that group, for a mistake dashsync itself never
// introduces (Merge only ever consults a name it's about to look up, not
// ones it invents). Revisit if this turns out to matter in practice.
func (NamedGroupAdapter) FindOrCreateGroup(groups *ast.SequenceNode, name string) (*ast.SequenceNode, error) {
	for _, v := range groups.Values {
		mn, ok := v.(*ast.MappingNode)
		if !ok {
			continue
		}
		nameField, ok := findField(mn, "name")
		if !ok {
			continue
		}
		got, ok := scalarString(nameField.Value)
		if !ok || got != name {
			continue
		}

		itemsField, err := getOrAppendField(mn, "items")
		if err != nil {
			return nil, fmt.Errorf("group %q: find or create items: %w", name, err)
		}
		seq, ok := itemsField.Value.(*ast.SequenceNode)
		if !ok {
			return nil, fmt.Errorf("group %q's items is not a list — dashsync can't merge into it", name)
		}
		fixFlowStyleColumn(seq, itemsField.Key.GetToken().Position.Column)
		// getOrAppendField already normalized mn (flow style and per-field
		// columns both) before finding or appending "items" above, so mn
		// itself is safe to splice block-style content into regardless of
		// whether it started flow-style.
		return seq, nil
	}

	mn, err := parseMappingNode(namedGroup{Name: name})
	if err != nil {
		return nil, fmt.Errorf("build new group %q: %w", name, err)
	}
	groups.IsFlowStyle = false
	groups.Values = append(groups.Values, mn)
	groups.ValueHeadComments = append(groups.ValueHeadComments, nil)

	itemsField, ok := findField(mn, "items")
	if !ok {
		return nil, fmt.Errorf("build new group %q: freshly built entry has no items field", name)
	}
	//nolint:forcetypeassert // namedGroup's own contract guarantees this shape
	seq := itemsField.Value.(*ast.SequenceNode)
	fixFlowStyleColumn(seq, itemsField.Key.GetToken().Position.Column)
	return seq, nil
}

// Groups implements DocumentAdapter.
func (NamedGroupAdapter) Groups(groups *ast.SequenceNode) []NamedGroup {
	out := make([]NamedGroup, 0, len(groups.Values))
	for _, v := range groups.Values {
		mn, ok := v.(*ast.MappingNode)
		if !ok {
			continue
		}
		nameField, ok := findField(mn, "name")
		if !ok {
			continue
		}
		name, ok := scalarString(nameField.Value)
		if !ok {
			continue
		}
		itemsField, ok := findField(mn, "items")
		if !ok {
			continue
		}
		seq, ok := itemsField.Value.(*ast.SequenceNode)
		if !ok {
			continue
		}
		out = append(out, NamedGroup{Name: name, Items: seq})
	}
	return out
}
