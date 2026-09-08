// Package homer renders dashsync's model into a Homer
// (github.com/bastienwirtz/homer) config.yml.
//
// Homer's managed content ("services:") lives nested amid otherwise-foreign
// hand-configured settings (title, theme, colors, ...) — a different shape
// from Homepage's, but one merge.NewNamedGroupAdapter knows how to walk;
// see docs/decisions/007-document-adapter.md.
package homer

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"

	"github.com/NikitaMikhailov/dashsync/internal/merge"
	"github.com/NikitaMikhailov/dashsync/internal/model"
)

// extraFieldPrefix keys, once "dashsync." is stripped (see
// internal/discovery's label contract), land directly on a service's item
// fields — e.g. "dashsync.homer.tag=app" becomes the item field "tag:
// app". Homer's items are flat objects (name/url/icon/subtitle/tag/type/
// ...), unlike Homepage's nested "widget:" block, so there's no separate
// sub-key to collect these under.
const extraFieldPrefix = "homer."

// document is Homer's config.yml shape, as far as dashsync is concerned:
// only the "services" key. A real config.yml has a great many other
// top-level settings (title, theme, colors, ...) that a human configures
// directly and this package never touches — Render always produces a
// document with services as its only key, meant to be pasted into (or,
// once merge support exists, merged into) a real config.yml, not written
// over one wholesale.
type document struct {
	Services []group `yaml:"services"`
}

// group is one entry under "services": a name and its items. Unlike
// Homepage's single-key-map shape, both group and item are ordinary
// multi-field objects — Homer identifies each by a "name" field, not by
// being a map's own key. model.Group carries no icon of its own, so there's
// no group-level icon field here; Homer items pick one up individually
// through itemFields.
type group struct {
	Name  string `yaml:"name"`
	Items []item `yaml:"items"`
}

// item is a service's fields as a generic map rather than a typed struct:
// a dashsync.homer.* label (see extraFields) can set any Homer item field
// there is — "type", "target", "class", ones this package has never heard
// of — and a fixed set of struct fields can't hold a key it doesn't
// declare. A map is what "arbitrary label-driven fields" requires today,
// independent of any future merge support for this format.
type item map[string]any

// Renderer renders dashsync's model as a Homer config.yml services list.
type Renderer struct{}

// Compile-time check that Renderer satisfies merge.EntryRenderer.
var _ merge.EntryRenderer = Renderer{}

// New returns a Homer Renderer.
func New() Renderer { return Renderer{} }

// Name implements render.Renderer.
func (Renderer) Name() string { return "homer" }

// DefaultPath implements render.Renderer.
func (Renderer) DefaultPath() string { return "config.yml" }

// Render implements render.Renderer. See that interface's doc comment for
// the ordering contract this relies on groups already satisfying.
func (Renderer) Render(groups []model.Group) ([]byte, error) {
	doc := document{Services: make([]group, 0, len(groups))}
	for _, g := range groups {
		items := make([]item, 0, len(g.Services))
		for _, s := range g.Services {
			items = append(items, itemFields(s))
		}
		doc.Services = append(doc.Services, group{Name: g.Name, Items: items})
	}

	out, err := yaml.MarshalWithOptions(doc, yamlOptions...)
	if err != nil {
		return nil, fmt.Errorf("render homer config.yml: %w", err)
	}
	return out, nil
}

// RenderEntry implements merge.EntryRenderer: it renders one service as a
// standalone Homer item entry, the granularity dashsync's idempotent merge
// (internal/merge) needs for inserting, updating, and hand-edit-detecting
// individual entries without touching the rest of an existing file.
func (Renderer) RenderEntry(s model.Service) ([]byte, error) {
	out, err := yaml.MarshalWithOptions(itemFields(s), yamlOptions...)
	if err != nil {
		return nil, fmt.Errorf("render homer entry for %q: %w", s.Name, err)
	}
	return out, nil
}

// NormalizeEntry implements merge.EntryRenderer: it decodes node — an
// existing entry as read back from a file — into the same generic shape
// RenderEntry builds, then re-renders it through the exact same encoder
// call. See homepage.Renderer's own NormalizeEntry doc comment for why
// this (rather than node.String()) is necessary, and why item is
// map[string]any rather than a typed struct — the same reasoning applies
// here unchanged.
func (Renderer) NormalizeEntry(node ast.Node) ([]byte, error) {
	var decoded map[string]any
	if err := yaml.NodeToValue(node, &decoded); err != nil {
		return nil, fmt.Errorf("normalize existing homer entry: %w", err)
	}
	out, err := yaml.MarshalWithOptions(decoded, yamlOptions...)
	if err != nil {
		return nil, fmt.Errorf("normalize existing homer entry: %w", err)
	}
	return out, nil
}

// Adapter implements merge.EntryRenderer: Homer's document shape (see this
// package's own doc comment) is exactly what merge.NewNamedGroupAdapter
// knows how to walk.
func (Renderer) Adapter() merge.DocumentAdapter { return merge.NewNamedGroupAdapter("services") }

// itemFields builds one service's Homer item fields. "name" is always
// present — Homer identifies an item by this field, unlike Homepage where
// the name is the map key and can never be missing by construction — so
// it's set unconditionally rather than through the same
// present-if-non-empty pattern the rest of these fields use.
//
// A dashsync.homer.* label can override any field here, "name" included:
// there's no special-casing to stop it, matching the same
// labels-win-if-present flexibility homepage.Renderer's widget passthrough
// allows.
func itemFields(s model.Service) item {
	fields := item{"name": s.Name}
	if s.URL != "" {
		fields["url"] = s.URL
	}
	if s.Icon != "" {
		fields["icon"] = s.Icon
	}
	if s.Description != "" {
		fields["subtitle"] = s.Description
	}
	for key, value := range extraFields(s.Extra) {
		fields[key] = value
	}
	return fields
}

// extraFields picks the "homer.*" keys out of extra and returns them
// keyed by whatever follows that prefix — "homer.tag" becomes the item
// field "tag", "homer.type" becomes "type" (Homer's smart-card selector,
// e.g. "PiHole" — see Homer's own docs for the available types).
//
// The result is a map[string]any, not something dashsync sorts itself
// before handing to yaml.Marshal — goccy/go-yaml sorts map keys
// alphabetically on encode regardless of the map's value type (see
// homepage.widgetFields' doc comment: verified against v1.19.2, no public
// guarantee of it in goccy's own docs, so a golden test failing on key
// order after a dependency bump is where to look first).
func extraFields(extra map[string]string) map[string]any {
	var fields map[string]any
	for key, value := range extra {
		suffix, ok := strings.CutPrefix(key, extraFieldPrefix)
		if !ok || suffix == "" {
			continue
		}
		if fields == nil {
			fields = make(map[string]any)
		}
		fields[suffix] = value
	}
	return fields
}

// yamlOptions pins the encoder's formatting the same way
// homepage.Renderer's does, and for the same reason: this output is meant
// to be committed to git, so its layout shouldn't drift on a dependency
// bump.
var yamlOptions = []yaml.EncodeOption{yaml.Indent(2)} //nolint:gochecknoglobals // read-only encoder config, not mutable state
