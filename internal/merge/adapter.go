package merge

import "github.com/goccy/go-yaml/ast"

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
// This is currently verified against exactly one shape (homepageAdapter,
// below) — see docs/decisions/007-document-adapter.md for what a second,
// foreign-nested shape (Homer/Dashy) will additionally require that isn't
// yet proven to fit here: specifically, parseOrEmpty's "no file yet"
// placeholder (a bare empty sequence) is Homepage's own shape, so
// GroupSequence's "bootstrap a fresh document" responsibility is trivial
// for homepageAdapter today and untested for any adapter whose document
// root isn't already shaped that way.
type DocumentAdapter interface {
	// GroupSequence returns the document's list of group entries. If file
	// has no real content yet (parseOrEmpty's placeholder), it's
	// responsible for building whatever minimal fresh document its shape
	// needs instead.
	GroupSequence(file *ast.File) (*ast.SequenceNode, error)

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

func (homepageAdapter) GroupSequence(file *ast.File) (*ast.SequenceNode, error) {
	return rootSequence(file)
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
