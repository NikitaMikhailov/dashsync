// Package dashy renders dashsync's model into a Dashy
// (github.com/Lissy93/dashy) conf.yml.
//
// Render-only for now, like homer.Renderer — see
// docs/decisions/007-document-adapter.md for the plan to give this format
// (and Homer) full idempotent merge support.
package dashy

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

// extraFieldPrefix keys, once "dashsync." is stripped (see
// internal/discovery's label contract), land directly on a service's item
// fields — e.g. "dashsync.dashy.target=newtab" becomes the item field
// "target: newtab". Dashy's items are flat objects (title/url/icon/
// description/target/statusCheck/tags/...), same shape as Homer's, so
// there's no separate sub-key to collect these under.
const extraFieldPrefix = "dashy."

// document is Dashy's conf.yml shape, as far as dashsync is concerned: only
// the "sections" key. A real conf.yml has pageInfo/appConfig/pages —
// hand-configured by a human — that this package never touches; Render
// always produces a document with sections as its only key, meant to be
// pasted into (or, once merge support exists, merged into) a real conf.yml,
// not written over one wholesale.
type document struct {
	Sections []group `yaml:"sections"`
}

// group is one entry under "sections": a name and its items. Like Homer's
// group (and unlike Homepage's single-key-map shape), this is an ordinary
// multi-field object — Dashy identifies a section by a "name" field, not
// by being a map's own key. model.Group carries no icon of its own, so
// there's no group-level icon field here.
type group struct {
	Name  string `yaml:"name"`
	Items []item `yaml:"items"`
}

// item is a service's fields as a generic map rather than a typed struct —
// same reasoning as homer.item: a dashsync.dashy.* label (see extraFields)
// can set any Dashy item field there is ("target", "statusCheck", "tags",
// ones this package has never heard of), and a fixed set of struct fields
// can't hold a key it doesn't declare.
type item map[string]any

// Renderer renders dashsync's model as a Dashy conf.yml sections list.
type Renderer struct{}

// New returns a Dashy Renderer.
func New() Renderer { return Renderer{} }

// Name implements render.Renderer.
func (Renderer) Name() string { return "dashy" }

// DefaultPath implements render.Renderer. "conf.yml" is Dashy's own
// documented default config file name (dashy.to/docs/quick-start.md).
func (Renderer) DefaultPath() string { return "conf.yml" }

// Render implements render.Renderer. See that interface's doc comment for
// the ordering contract this relies on groups already satisfying.
func (Renderer) Render(groups []model.Group) ([]byte, error) {
	doc := document{Sections: make([]group, 0, len(groups))}
	for _, g := range groups {
		items := make([]item, 0, len(g.Services))
		for _, s := range g.Services {
			items = append(items, itemFields(s))
		}
		doc.Sections = append(doc.Sections, group{Name: g.Name, Items: items})
	}

	out, err := yaml.MarshalWithOptions(doc, yamlOptions...)
	if err != nil {
		return nil, fmt.Errorf("render dashy conf.yml: %w", err)
	}
	return out, nil
}

// itemFields builds one service's Dashy item fields. "title" is always
// present — Dashy identifies an item by this field (not "name", the one
// real point of divergence from Homer's otherwise-identical shape) — so
// it's set unconditionally rather than through the same
// present-if-non-empty pattern the rest of these fields use.
//
// A dashsync.dashy.* label can override any field here, "title" included:
// there's no special-casing to stop it, matching the same
// labels-win-if-present flexibility homer.Renderer's passthrough allows.
func itemFields(s model.Service) item {
	fields := item{"title": s.Name}
	if s.URL != "" {
		fields["url"] = s.URL
	}
	if s.Icon != "" {
		fields["icon"] = s.Icon
	}
	if s.Description != "" {
		fields["description"] = s.Description
	}
	for key, value := range extraFields(s.Extra) {
		fields[key] = value
	}
	return fields
}

// extraFields picks the "dashy.*" keys out of extra and returns them keyed
// by whatever follows that prefix — "dashy.target" becomes the item field
// "target" (Dashy's link-open-behavior option, e.g. "newtab"). A Docker
// label's value is always a plain string (model.Service.Extra is
// map[string]string — that's all a label can ever be), so this can only
// ever set a scalar field: "dashy.tags", say, comes out as the literal
// string "dns,admin", never Dashy's real array-valued `tags: [dns, admin]`
// form. Same limitation as homepage.entryFields' widget passthrough, for
// the same reason — future work with no caller yet, not a case this
// prefix mechanism can express today.
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
