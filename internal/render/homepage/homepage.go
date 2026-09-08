// Package homepage renders dashsync's model into a Homepage
// (gethomepage.dev) services.yaml.
package homepage

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

// widgetLabelPrefix keys, once "dashsync." is stripped (see
// internal/discovery's label contract), collect into a service's widget
// block — e.g. "dashsync.homepage.widget.type=jellyfin" becomes
// widget: {type: jellyfin}.
const widgetLabelPrefix = "homepage.widget."

// entry is one service's body under its name in services.yaml. Homepage
// supports a good deal more per-widget than a flat string map (typed
// fields, highlight rules, ...) — Widget only carries what a label can
// express as a bare key=value pair, which covers the common widgets
// (type/url/key) but not array-valued options like a widget's `fields`
// list. Extending this to that is future work with no caller yet.
type entry struct {
	Href        string            `yaml:"href,omitempty"`
	Icon        string            `yaml:"icon,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Widget      map[string]string `yaml:"widget,omitempty"`
}

// service and group are the single-key-map shape every list item in
// Homepage's config format uses: a service named by its one key, a group
// the same way one level up. A one-entry map carries no ordering
// ambiguity, which is what makes this shape workable without
// goccy/go-yaml's ordered-map support (that's reserved for M3, where an
// *existing* file's key order and comments have to survive a merge).
type service map[string]entry

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
			services = append(services, oneEntry(s.Name, entry{
				Href:        s.URL,
				Icon:        s.Icon,
				Description: s.Description,
				Widget:      widgetFields(s.Extra),
			}))
		}
		doc = append(doc, oneEntry(g.Name, services))
	}

	// Indent is pinned explicitly rather than left to goccy's default:
	// this output is meant to be committed to git and, from M3 on, merged
	// back into by re-parsing it, so its layout is a stability contract,
	// not an incidental library default that could shift on the next
	// dependency bump. IndentSequence is deliberately NOT set: turning it
	// on shifts the top-level sequence itself in by one indent level,
	// which breaks Homepage's expected "- Group:" starting at column 0.
	out, err := yaml.MarshalWithOptions(doc, yaml.Indent(2))
	if err != nil {
		return nil, fmt.Errorf("render homepage services.yaml: %w", err)
	}
	return out, nil
}

// widgetFields picks the "homepage.widget.*" keys out of extra and returns
// them keyed by whatever follows that prefix — "homepage.widget.type"
// becomes the map key "type". Returns nil (which marshals to nothing,
// thanks to entry.Widget's omitempty) if extra has none.
//
// The result is a map[string]string, not something dashsync sorts itself
// before handing to yaml.Marshal — goccy/go-yaml sorts map[string]string
// keys alphabetically on encode (verified against v1.19.2; there's no
// public guarantee of this in its docs, so if a golden test here ever
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
