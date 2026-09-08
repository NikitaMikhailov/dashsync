package merge

import (
	"bytes"
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// seqEntry is one item of a YAML block sequence, decomposed into its head
// comment (if any) and value. It exists to work around a quirk in
// goccy/go-yaml's AST: the head comment for a sequence's *first* entry
// lives on the sequence node itself (seq.GetComment()), while every other
// entry's head comment lives in the parallel seq.ValueHeadComments slice.
// Reading into, and writing back from, this plain slice means the rest of
// this package never has to think about that special case more than once.
type seqEntry struct {
	comment *ast.CommentGroupNode
	value   ast.Node
}

// readEntries decomposes seq into a plain slice — see seqEntry.
func readEntries(seq *ast.SequenceNode) []seqEntry {
	entries := make([]seqEntry, len(seq.Values))
	for i, v := range seq.Values {
		var c *ast.CommentGroupNode
		switch {
		case i == 0:
			c = seq.GetComment()
		case i < len(seq.ValueHeadComments):
			c = seq.ValueHeadComments[i]
		}
		entries[i] = seqEntry{comment: c, value: v}
	}
	return entries
}

// writeEntries writes entries back into seq — the inverse of readEntries,
// handling the same first-entry special case.
//
// An empty entries list is special-cased: rather than clearing seq's
// existing Values in place, seq is replaced wholesale with a freshly
// parsed "[]". A sequence that held real block-style entries a moment ago
// carries token metadata (indent level, not just column) matching that
// depth; MappingValueNode.String() consults indent level — not just
// whether the sequence is empty — to decide between "key: []" on one line
// and "key:\n<value>" on the next, so leaving that stale metadata in place
// on an emptied sequence renders it as the latter with nothing after it,
// silently dropping the "[]" the moment a group loses its last entry.
// Building a genuinely fresh node sidesteps every field this could
// possibly depend on, rather than chasing them down one at a time.
func writeEntries(seq *ast.SequenceNode, entries []seqEntry) error {
	if len(entries) == 0 {
		empty, err := newEmptySequence()
		if err != nil {
			return err
		}
		*seq = *empty
		return nil
	}

	// seq itself (the items list) always gets un-flow-styled here — but
	// note this is the *items* sequence, not the *group entry* mapping
	// that contains it ("- {name: Media, items: [...]}"). The add path
	// (FindOrCreateGroup) separately normalizes that enclosing mapping via
	// normalizeMappingFlowStyle before ever reaching here, for exactly the
	// class of bug documented in docs/decisions/007-document-adapter.md.
	// The removal pass (Merge's second loop) reaches writeEntries directly
	// for every existing group, including ones FindOrCreateGroup never
	// touched this run, without that same normalization — safe in
	// practice only because a flow-style group entry with an already-
	// marker-tagged, removable orphan can't arise organically (a marker
	// inside a flow-style items list isn't recognized as a marker at all,
	// per docs/decisions/002-merge-strategy.md's own known limitations),
	// not because this path actively guards against it.
	seq.IsFlowStyle = false
	seq.Values = make([]ast.Node, len(entries))
	seq.ValueHeadComments = make([]*ast.CommentGroupNode, len(entries))

	for i, e := range entries {
		seq.Values[i] = e.value
		if i == 0 {
			_ = seq.SetComment(e.comment) // BaseNode.SetComment never errors
			continue
		}
		seq.ValueHeadComments[i] = e.comment
	}
	return nil
}

// newEmptySequence returns a fresh empty flow-style sequence node ("[]"),
// with token metadata as consistent as if it had been parsed as part of a
// real document — because it was, just a throwaway one-line one.
func newEmptySequence() (*ast.SequenceNode, error) {
	f, err := parser.ParseBytes([]byte("[]\n"), parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("build empty sequence: %w", err)
	}
	//nolint:forcetypeassert // "[]\n" always parses as a *ast.SequenceNode
	return f.Docs[0].Body.(*ast.SequenceNode), nil
}

// parseOrEmpty parses existing as a dashsync-managed document, treating
// empty (or all-whitespace) input as "no file yet" — an empty group
// list — rather than a parse error.
func parseOrEmpty(existing []byte) (*ast.File, error) {
	if len(bytes.TrimSpace(existing)) == 0 {
		existing = []byte("[]\n")
	}
	return parser.ParseBytes(existing, parser.ParseComments)
}

// rootSequence returns a document's top-level sequence — dashsync's list
// of groups — erroring clearly if it isn't shaped like one at all, rather
// than panicking on a failed type assertion deeper in Merge.
func rootSequence(file *ast.File) (*ast.SequenceNode, error) {
	if len(file.Docs) == 0 || file.Docs[0].Body == nil {
		return nil, fmt.Errorf("existing config is empty of structure entirely — this shouldn't happen after parseOrEmpty")
	}
	seq, ok := file.Docs[0].Body.(*ast.SequenceNode)
	if !ok {
		return nil, fmt.Errorf("existing config's top level is a %s, not a list of groups — dashsync can't merge into it",
			file.Docs[0].Body.Type())
	}
	return seq, nil
}

// nodeKeyString extracts a mapping key's semantic string value — not its
// source text, which GetValue's sibling method String() would return
// including quotes if the key needed any (e.g. a name containing ":").
// Comparing against String() instead of this would silently fail to match
// any group or service name needing YAML quoting.
func nodeKeyString(key ast.MapKeyNode) (string, bool) {
	scalar, ok := key.(ast.ScalarNode)
	if !ok {
		return "", false
	}
	s, ok := scalar.GetValue().(string)
	return s, ok
}

// findOrCreateGroupSequence finds the sequence of services for group name
// among root's existing entries, matched by key — an entry whose value
// isn't a well-formed "Name: [...]" mapping simply never matches, so it's
// left exactly where it is and never touched. If no entry matches, a
// fresh, empty one is appended to root.
func findOrCreateGroupSequence(root *ast.SequenceNode, name string) (*ast.SequenceNode, error) {
	for _, v := range root.Values {
		mn, ok := v.(*ast.MappingNode)
		if !ok || len(mn.Values) == 0 {
			continue
		}
		mvn := mn.Values[0]
		key, ok := nodeKeyString(mvn.Key)
		if !ok || key != name {
			continue
		}
		seq, ok := mvn.Value.(*ast.SequenceNode)
		if !ok {
			return nil, fmt.Errorf("group %q's value is not a list of services — dashsync can't merge into it", name)
		}
		fixFlowStyleColumn(seq, mvn.Key.GetToken().Position.Column)
		// This group entry is about to have Merge's own writeEntries splice
		// block-style content into seq — a flow-style entry ("- {Media:
		// [...]}") can't legally hold that, the same reason root itself
		// gets normalized in Merge. Homepage's own mn always has exactly
		// one field by construction (single-key map), so there's no
		// sibling-column-consistency concern normalizeMappingFlowStyle
		// otherwise exists for — reused anyway for the IsFlowStyle
		// correction it also does, rather than duplicating a one-line
		// version of it here.
		normalizeMappingFlowStyle(mn)
		return seq, nil
	}

	node, err := parseGroupNode(name)
	if err != nil {
		return nil, fmt.Errorf("build new group %q: %w", name, err)
	}
	root.IsFlowStyle = false
	root.Values = append(root.Values, node)
	root.ValueHeadComments = append(root.ValueHeadComments, nil)

	//nolint:forcetypeassert // parseGroupNode's own contract guarantees this shape
	seq := node.Values[0].Value.(*ast.SequenceNode)
	fixFlowStyleColumn(seq, node.Values[0].Key.GetToken().Position.Column)
	return seq, nil
}

// fixFlowStyleColumn corrects a flow-style sequence's Start column before
// entries get appended to it and it's flipped to block style — whether
// it's currently empty ("Media: []") or already holds hand-written
// content in flow form ("Media: [ManualThing: {href: ...}]"), since both
// carry the same problem: a Start column pointing at wherever "[" happened
// to sit in whatever one-line source produced this node, rather than
// anywhere useful for block-style indentation.
//
// The correct column is derived from keyColumn — the column of the
// mapping key this sequence is the value of ("Media" in "Media: []") —
// rather than a fixed constant: that key's own column varies with how
// deeply it's already nested (column 1 for a brand-new group parsed as
// its own standalone document, column 3 for an existing "- Media: []"
// entry once it's back at the top of the real file, and so on), and a
// sequence's column needs to track that to produce a consistent indent
// width once real entries render under it.
//
// Left uncorrected, block-style rendering indents every entry in the
// sequence by that stale column, uniformly — regardless of which node
// produced which entry — producing wildly over-indented output (a fresh
// group, or any hand-collapsed flow-style one) the moment a real entry is
// appended. This was found and fixed for the empty case first, then
// generalized here once the same symptom turned up on a non-empty
// flow-style group — see merge_test.go's
// TestMerge_ExistingFlowStyleGroupGetsBlockStyleIndentation for the
// regression this closes.
//
// +2 matches this package's own convention (2-space indent) for a
// sequence nested one level under its mapping key — the shape every
// group's services list has, since dashsync's model doesn't support
// nested groups. The parent's own block-style rendering re-derives the
// actual final indentation relative to this, the same way it already does
// for every entry parsed as its own standalone document elsewhere in this
// package — so this is safe even though it doesn't know how deep in the
// real document this group itself ends up. Existing entries already in
// the sequence are untouched by this: their own internal structure is
// self-consistent regardless of the parent's column, exactly like every
// other node this package splices in from a separate parse.
func fixFlowStyleColumn(seq *ast.SequenceNode, keyColumn int) {
	if seq.IsFlowStyle {
		seq.Start.Position.Column = keyColumn + 2
	}
}

// normalizeMappingFlowStyle forces mn to block style, and recolumns every
// one of its fields to a consistent baseline (column 1, matching a
// freshly-parsed standalone mapping — see parseMappingNode) if mn was
// flow-style. A no-op if it wasn't.
//
// A flow-style mapping's fields carry mutually inconsistent columns from
// wherever they happened to sit on one line — "{name: Media, items: []}"
// puts "name" right after "{" and "items" much further along the same
// line, nothing like the aligned columns two sibling fields have in a
// normal block-style parse. That mismatch matters because
// ast.SequenceNode's own block-style rendering (blockStyleString) doesn't
// re-derive each line's indentation independently: it measures how many
// leading spaces the *first* line of an entry's rendered text has, then
// strips exactly that many characters from every subsequent line before
// re-indenting the whole entry uniformly. Two fields sharing one column
// (the normal case) survive that unchanged; two fields whose original
// columns differ by roughly the same margin their flow-style positions
// differed by do not — simply flipping IsFlowStyle without also fixing
// this reproduces the same garbled-indentation symptom
// fixFlowStyleColumn's own doc comment already describes for a sequence's
// own Start column, one level down, on a mapping's fields instead. Found
// via TestNamedGroupAdapter_FindOrCreateGroup_ConvertsFlowStyleGroupsListToBlockStyle,
// which failed on IsFlowStyle correction alone before this was added.
func normalizeMappingFlowStyle(mn *ast.MappingNode) {
	if !mn.IsFlowStyle {
		return
	}
	mn.IsFlowStyle = false
	for _, field := range mn.Values {
		field.AddColumn(1 - field.Key.GetToken().Position.Column)
	}
}

// normalizeGroupsListStyle forces groups to block style once it holds at
// least one real group. A hand-written groups list in flow style
// ("[{name: Media, items: []}]", or Homepage's "[{Media: []}]") can't
// legally contain the block-style content (a marker comment, a multi-line
// entry) Merge is about to splice into one of its groups' items —
// flow-style YAML can't hold block-style children at all, regardless of
// which group actually changes this run. This is the same policy
// fixFlowStyleColumn's own doc comment already established for a group's
// items list, applied here to the outermost groups container: found only
// once a document with a hand-written flow-style groups list at the root
// was actually tried, not anticipated in the original design (see
// docs/decisions/007-document-adapter.md).
//
// Called from each DocumentAdapter's own GroupSequence, not centrally from
// Merge, so a direct test against GroupSequence alone (rather than the
// whole Merge pipeline) can verify it — see adapter_test.go.
func normalizeGroupsListStyle(groups *ast.SequenceNode) {
	if len(groups.Values) > 0 {
		groups.IsFlowStyle = false
	}
}

// parseGroupNode builds a fresh "name: []" top-level entry. Going through
// yaml.Marshal rather than building the YAML text by hand (fmt.Sprintf)
// means a name needing quoting (containing ":", starting with a YAML
// special character, ...) comes out correctly quoted — the same reason
// homepage.Renderer never hand-builds YAML text either.
func parseGroupNode(name string) (*ast.MappingNode, error) {
	return parseMappingNode(map[string][]struct{}{name: {}})
}

// parseEntryNode renders a single entry's marshaled bytes (as produced by
// an EntryRenderer) into the AST node Merge splices into a group's
// sequence.
func parseEntryNode(rendered []byte) (ast.Node, error) {
	f, err := parser.ParseBytes(rendered, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse rendered entry: %w", err)
	}
	if f.Docs[0].Body == nil {
		return nil, fmt.Errorf("rendered entry is empty")
	}
	return f.Docs[0].Body, nil
}

// parseMappingNode marshals v and parses the result back into a
// *ast.MappingNode — the shared plumbing behind parseGroupNode.
func parseMappingNode(v any) (*ast.MappingNode, error) {
	out, err := yaml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	f, err := parser.ParseBytes(out, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	mn, ok := f.Docs[0].Body.(*ast.MappingNode)
	if !ok {
		return nil, fmt.Errorf("marshaled %T did not parse back as a mapping", v)
	}
	return mn, nil
}

// scalarString extracts n's scalar string value. Unlike nodeKeyString, n is
// a plain mapping *value*, not a map key, so it isn't constrained to
// ast.MapKeyNode.
//
// Known limitation: *ast.AliasNode satisfies ast.ScalarNode, but its
// GetValue() returns the alias's own name text ("g" for "*g"), not the
// value the anchor it points to actually holds — a "name: &g Media" anchor
// resolves correctly (the anchor node itself carries the real value), but
// a later "name: *g" alias reference to it wouldn't. Hand-anchoring a
// group's own name field is exotic enough not to special-case; flagged
// here rather than silently assumed to work.
func scalarString(n ast.Node) (string, bool) {
	scalar, ok := n.(ast.ScalarNode)
	if !ok {
		return "", false
	}
	s, ok := scalar.GetValue().(string)
	return s, ok
}

// findField returns mn's field named key, or false if absent — a
// *ast.MappingNode has no faster lookup than scanning in this AST.
func findField(mn *ast.MappingNode, key string) (*ast.MappingValueNode, bool) {
	for _, v := range mn.Values {
		if k, ok := nodeKeyString(v.Key); ok && k == key {
			return v, true
		}
	}
	return nil, false
}

// getOrAppendField finds mn's field named key, or appends a fresh "key: []"
// field (an empty sequence value) if none exists yet — preserving every
// other field already in mn untouched, in its original position. Used both
// for a document's top-level "services:"/"sections:" key and for a group
// entry's own "items:" key when a hand-written group predates dashsync
// managing it.
//
// A fresh field, parsed as its own standalone one-line document, starts at
// column 1 — MappingValueNode.String() derives a field's leading
// whitespace directly from its own key's absolute column, unlike a
// *ast.SequenceNode entry's leading "- ", which the parent sequence's own
// rendering strips regardless of the entry's own column (see
// fixFlowStyleColumn's doc comment for that contrast: this is genuinely
// different mechanics, not the same fix applied twice). Appending this
// field next to mn's existing ones without correcting that would render it
// flush against the left margin, ignoring however deep mn itself is
// nested. ast.Node.AddColumn — the same primitive goccy/go-yaml's own
// ast.MappingNode.Merge method uses for an identical purpose — shifts the
// whole freshly-parsed subtree (key, value, and everything under it) by a
// column delta in one call.
//
// Correcting Column alone is sufficient for the field being appended,
// verified against goccy/go-yaml@v1.19.2's own source
// (MappingValueNode.toString): the choice between "key: []" on one line
// and "key:\n  []" on the next compares this field's own key IndentLevel
// against its own value's IndentLevel — never against a sibling field's —
// and AddColumn doesn't touch IndentLevel at all, so that self-contained
// comparison stays exactly as it was in the fresh one-line parse that
// already renders correctly on its own (the same "name: []" shape
// parseGroupNode already relies on elsewhere in this file). See
// getorappendfield_test.go for the empirical check backing this, not just
// the source-reading.
//
// normalizeMappingFlowStyle runs first, unconditionally, for a separate
// reason: if mn itself was flow-style ("{name: Media}"), its *existing*
// fields carry mutually inconsistent columns from wherever they sat on one
// line, which corrupts block-style rendering once any of them spans
// multiple lines — independent of whatever field this call is about to
// find or append. See that function's own doc comment for the mechanism.
func getOrAppendField(mn *ast.MappingNode, key string) (*ast.MappingValueNode, error) {
	normalizeMappingFlowStyle(mn)

	if f, ok := findField(mn, key); ok {
		return f, nil
	}

	fresh, err := parseMappingNode(map[string][]struct{}{key: {}})
	if err != nil {
		return nil, fmt.Errorf("build new %q field: %w", key, err)
	}
	mvn := fresh.Values[0]

	targetColumn := 1
	if len(mn.Values) > 0 {
		targetColumn = mn.Values[0].Key.GetToken().Position.Column
	}
	mvn.AddColumn(targetColumn - 1)

	mn.Values = append(mn.Values, mvn)
	return mvn, nil
}
