// Package homepage renders dashsync's model into a Homepage
// (gethomepage.dev) services.yaml.
package homepage

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

// widgetLabelPrefix keys, once "dashsync." is stripped (see
// internal/discovery's label contract), collect into a service's widget
// block — e.g. "dashsync.homepage.widget.type=jellyfin" becomes
// widget: {type: jellyfin}.
const widgetLabelPrefix = "homepage.widget."

// service and group are the single-key-map shape every list item in
// Homepage's config format uses: a service named by its one key, a group
// the same way one level up. A one-entry map carries no ordering
// ambiguity, which is what makes this shape workable without
// goccy/go-yaml's ordered-map support (that's reserved for the file-level
// merge in internal/merge, where an *existing* file's key order and
// comments have to survive).
//
// A service's own fields (service's value type) are a generic
// map[string]any, not a typed struct: internal/merge's hand-edit detection
// (NormalizeEntry, below) depends on RenderEntry and NormalizeEntry
// producing byte-identical output for byte-identical content, and a typed
// struct forces that through two independent orderings — Go's
// struct-field declaration order for encoding, and goccy's alphabetical
// map-key sort for decoding back through NodeToValue — that have no
// reason to agree unless someone remembers to keep the struct fields
// declared in alphabetical order by hand, forever, including any field
// added later. Building the map directly means there's only one ordering
// in play, decided once by
// goccy's own encoder, and used identically on both sides of the
// comparison. It also means every field an existing entry actually has —
// not just the ones dashsync's Service models — round-trips through
// NormalizeEntry rather than being silently dropped by a struct decode
// that only knows about href/icon/description/widget.
type service map[string]map[string]any

type group map[string][]service

// oneEntry builds a single-key map — a service or a group entry — through
// one constructor instead of a map literal at each call site. Go's type
// system can't express "exactly one entry" on map[string]V itself, so
// keeping construction in one place is what stands in for that guarantee.
func oneEntry[V any](name string, value V) map[string]V {
	return map[string]V{name: value}
}

// Renderer renders dashsync's model as a Homepage services.yaml.
type Renderer struct{}

// New returns a Homepage Renderer.
func New() Renderer { return Renderer{} }

// Name implements render.Renderer.
func (Renderer) Name() string { return "homepage" }

// DefaultPath implements render.Renderer.
func (Renderer) DefaultPath() string { return "services.yaml" }

// Render implements render.Renderer. See that interface's doc comment for
// the ordering contract this relies on groups already satisfying.
func (Renderer) Render(groups []model.Group) ([]byte, error) {
	doc := make([]group, 0, len(groups))
	for _, g := range groups {
		services := make([]service, 0, len(g.Services))
		for _, s := range g.Services {
			services = append(services, oneEntry(s.Name, entryFields(s)))
		}
		doc = append(doc, oneEntry(g.Name, services))
	}

	out, err := yaml.MarshalWithOptions(doc, yamlOptions...)
	if err != nil {
		return nil, fmt.Errorf("render homepage services.yaml: %w", err)
	}
	return out, nil
}

// RenderEntry implements merge.EntryRenderer: it renders one service as a
// standalone "Name:\n  field: value\n" entry, the granularity dashsync's
// idempotent merge (internal/merge) needs for inserting, updating, and
// hand-edit-detecting individual entries without touching the rest of an
// existing file.
func (Renderer) RenderEntry(s model.Service) ([]byte, error) {
	out, err := yaml.MarshalWithOptions(oneEntry(s.Name, entryFields(s)), yamlOptions...)
	if err != nil {
		return nil, fmt.Errorf("render homepage entry for %q: %w", s.Name, err)
	}
	return out, nil
}

// NormalizeEntry implements merge.EntryRenderer: it decodes node — an
// existing entry as read back from a file — into the same generic shape
// RenderEntry builds, then re-renders it through the exact same encoder
// call. Two things make this necessary rather than just calling
// node.String():
//
//  1. A node's own String() reflects the column it happened to be parsed
//     at, which varies with how deeply it's nested in the surrounding
//     document — decoding to a Go value and re-marshaling from scratch is
//     what makes "what's already in the file" and "what dashsync would
//     write" byte-comparable regardless of that.
//  2. Decoding into map[string]any rather than a typed struct means a
//     field this package doesn't know about — a human hand-adding a
//     Homepage widget option dashsync's Service has no place for, say —
//     still round-trips into the comparison, instead of silently
//     vanishing from it (and then from the file, the next time an
//     unrelated field changes and this entry gets rewritten). See
//     service's own doc comment for why map[string]any specifically, not
//     just "a generic type."
func (Renderer) NormalizeEntry(node ast.Node) ([]byte, error) {
	var decoded map[string]any
	if err := yaml.NodeToValue(node, &decoded); err != nil {
		return nil, fmt.Errorf("normalize existing homepage entry: %w", err)
	}
	out, err := yaml.MarshalWithOptions(decoded, yamlOptions...)
	if err != nil {
		return nil, fmt.Errorf("normalize existing homepage entry: %w", err)
	}
	return out, nil
}

// entryFields builds a service's fields as a generic map — see service's
// doc comment for why a map instead of a typed struct. Homepage supports
// a good deal more per-widget than a flat string map (typed fields,
// highlight rules, ...) — the widget value here only carries what a label
// can express as a bare key=value pair, which covers the common widgets
// (type/url/key) but not array-valued options like a widget's `fields`
// list. Extending that is future work with no caller yet.
func entryFields(s model.Service) map[string]any {
	fields := map[string]any{}
	if s.URL != "" {
		fields["href"] = s.URL
	}
	if s.Icon != "" {
		fields["icon"] = s.Icon
	}
	if s.Description != "" {
		fields["description"] = s.Description
	}
	if widget := widgetFields(s.Extra); widget != nil {
		fields["widget"] = widget
	}
	return fields
}

// yamlOptions is shared by every encode call in this package so a
// whole-document render, a single-entry render, and a normalized existing
// entry never drift into different formatting from each other — the
// byte-for-byte comparisons internal/merge does between them depend on
// all three producing output the exact same way.
//
// Indent is pinned explicitly rather than left to goccy's default: this
// output is meant to be committed to git and re-parsed for merging, so its
// layout is a stability contract, not an incidental library default that
// could shift on the next dependency bump. IndentSequence is deliberately
// NOT set: turning it on shifts a top-level sequence in by one indent
// level, which breaks Homepage's expected "- Group:" starting at column 0.
var yamlOptions = []yaml.EncodeOption{yaml.Indent(2)} //nolint:gochecknoglobals // read-only encoder config, not mutable state

// widgetFields picks the "homepage.widget.*" keys out of extra and returns
// them keyed by whatever follows that prefix — "homepage.widget.type"
// becomes the map key "type". Returns nil if extra has none, which
// entryFields treats as "no widget key at all" rather than an empty one.
//
// The result is a map[string]string, not something dashsync sorts itself
// before handing to yaml.Marshal — goccy/go-yaml sorts map keys
// alphabetically on encode regardless of the map's value type (verified
// against v1.19.2 for both map[string]string and map[string]any; there's
// no public guarantee of this in its docs, so if a golden test here ever
// starts failing on key order after a dependency bump, that assumption is
// where to look first).
func widgetFields(extra map[string]string) map[string]string {
	var widget map[string]string
	for key, value := range extra {
		suffix, ok := strings.CutPrefix(key, widgetLabelPrefix)
		if !ok || suffix == "" {
			continue
		}
		if widget == nil {
			widget = make(map[string]string)
		}
		widget[suffix] = value
	}
	return widget
}
