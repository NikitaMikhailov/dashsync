package homepage

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

var update = flag.Bool("update", false, "update golden files")

func TestRenderer_Name(t *testing.T) {
	t.Parallel()

	if got := New().Name(); got != "homepage" {
		t.Errorf("Name() = %q, want %q", got, "homepage")
	}
}

func TestRenderer_DefaultPath(t *testing.T) {
	t.Parallel()

	if got := New().DefaultPath(); got != "services.yaml" {
		t.Errorf("DefaultPath() = %q, want %q", got, "services.yaml")
	}
}

func TestRenderer_Render_Golden(t *testing.T) {
	t.Parallel()

	groups := []model.Group{
		{
			Name: "Media",
			Services: []model.Service{
				{Name: "Jellyfin", URL: "http://10.0.0.5:8096", Icon: "jellyfin", Description: "Movies & TV"},
				{Name: "Sonarr", URL: "http://10.0.0.5:8989"},
			},
		},
		{
			Name: "Network",
			Services: []model.Service{
				{Name: "AdGuard Home", URL: "http://10.0.0.5:3000"},
			},
		},
	}

	assertGolden(t, "services.golden.yaml", groups)
}

func TestRenderer_Render_EmptyService(t *testing.T) {
	t.Parallel()

	// A service with no URL, icon, or description at all — omitempty must
	// drop every one of those keys rather than emit them empty.
	groups := []model.Group{
		{Name: "Other", Services: []model.Service{{Name: "internal-svc"}}},
	}

	assertGolden(t, "empty_fields.golden.yaml", groups)
}

func TestRenderer_Render_Widget(t *testing.T) {
	t.Parallel()

	// Extra label keys not seen before by this test file: "type", "url",
	// and "key" collected out of dashsync.homepage.widget.* labels. Keys
	// deliberately inserted out of alphabetical order in the map literal —
	// map construction order can't influence output order in Go, but this
	// makes clear the test isn't accidentally passing because of it.
	//
	// "homer.tag" carries a different format's prefix entirely (as a
	// dashsync.homer.tag label would produce) — proof extraFields only
	// ever picks up this renderer's own "homepage." keys, not another
	// format's, even though both land in the same Service.Extra map.
	groups := []model.Group{
		{
			Name: "Media",
			Services: []model.Service{
				{
					Name: "Jellyfin",
					URL:  "http://10.0.0.5:8096",
					Extra: map[string]string{
						"homepage.widget.url":  "http://10.0.0.5:8096",
						"homepage.widget.type": "jellyfin",
						"homepage.widget.key":  "abc123",
						"homer.tag":            "should not leak into Homepage's output: wrong format's prefix",
					},
				},
			},
		},
	}

	assertGolden(t, "widget.golden.yaml", groups)
}

func TestRenderer_Render_FlatExtraFields(t *testing.T) {
	t.Parallel()

	// dashsync.homepage.* labels that aren't under widget. land directly
	// on the service, unnested — this is what a Docker-stats card needs
	// (server/container/showStats are siblings of href, not a widget
	// block; see docs/decisions/001-label-schema.md).
	groups := []model.Group{
		{
			Name: "Websites",
			Services: []model.Service{
				{
					Name: "Chainle",
					URL:  "https://chainle.ru",
					Extra: map[string]string{
						"homepage.server":    "my-docker",
						"homepage.container": "chainleru-web-1",
						"homepage.showStats": "true",
					},
				},
			},
		},
	}

	assertGolden(t, "flat_extra_fields.golden.yaml", groups)
}

func TestRenderer_Render_FlatExtraFieldCanOverrideAKnownField(t *testing.T) {
	t.Parallel()

	// Matches homer.Renderer's precedent: a label wins over any field
	// dashsync would otherwise have derived on its own — href, icon, and
	// description alike, no special-casing any of them. Useful when a
	// service needs, say, a different icon specifically for the Homepage
	// card. All three are exercised here, not just one: entryFields
	// assigns them through the identical unconditional "fields[key] =
	// value" loop, but nothing stops a future change from special-casing
	// just one of them without the others noticing.
	groups := []model.Group{
		{
			Name: "Media",
			Services: []model.Service{
				{
					Name:        "Jellyfin",
					URL:         "http://10.0.0.5:8096",
					Icon:        "jellyfin",
					Description: "Movies & TV",
					Extra: map[string]string{
						"homepage.href":        "http://10.0.0.5:9000",
						"homepage.icon":        "jellyfin-alt",
						"homepage.description": "Movies & TV, overridden",
					},
				},
			},
		},
	}

	assertGolden(t, "flat_extra_field_override.golden.yaml", groups)
}

func TestRenderer_Render_WidgetTypoDoesNotLeakAsAFlatField(t *testing.T) {
	t.Parallel()

	// "homepage.widgetXYZ" is one dropped dot away from a real
	// "homepage.widget.xyz" widget option. Without an explicit guard,
	// CutPrefix(key, "homepage.") alone would happily produce the flat
	// field "widgetXYZ" — a nonsense top-level key nobody meant to write,
	// silently accepted instead of either becoming the widget option the
	// label was aiming for or failing loudly.
	groups := []model.Group{
		{
			Name: "Media",
			Services: []model.Service{
				{
					Name: "Jellyfin",
					Extra: map[string]string{
						"homepage.widgetXYZ": "should not appear anywhere in the output",
					},
				},
			},
		},
	}

	assertGolden(t, "widget_typo_dropped.golden.yaml", groups)
}

func TestRenderer_Render_BareWidgetExtraKeyDoesNotClobberWidgetBlock(t *testing.T) {
	t.Parallel()

	// A "dashsync.homepage.widget" label (no further nesting) has suffix
	// "widget" once "homepage." is stripped — indistinguishable, by
	// prefix alone, from the reserved "widget" field flatExtraFields
	// builds from "homepage.widget.*" keys. Without an explicit guard,
	// map iteration order would nondeterministically decide whether the
	// real widget block or this stray string wins.
	groups := []model.Group{
		{
			Name: "Media",
			Services: []model.Service{
				{
					Name: "Jellyfin",
					Extra: map[string]string{
						"homepage.widget.type": "jellyfin",
						"homepage.widget":      "should be dropped, not clobber the widget block",
					},
				},
			},
		},
	}

	assertGolden(t, "bare_widget_key_dropped.golden.yaml", groups)
}

func TestRenderer_Render_SingleKeyInvariant(t *testing.T) {
	t.Parallel()

	// Two groups, two services each, so a bug that merged entries into a
	// shared map (defeating the point of the oneEntry constructor) would
	// show up as a group or service list shrinking instead of just
	// reordering.
	groups := []model.Group{
		{Name: "Media", Services: []model.Service{{Name: "Jellyfin"}, {Name: "Sonarr"}}},
		{Name: "Network", Services: []model.Service{{Name: "AdGuard Home"}, {Name: "Pi-hole"}}},
	}

	out, err := New().Render(groups)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	var decoded []map[string][]map[string]any
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output did not parse as YAML: %v\noutput:\n%s", err, out)
	}

	if len(decoded) != len(groups) {
		t.Fatalf("decoded %d groups, want %d (a single-key violation would merge groups together)", len(decoded), len(groups))
	}
	for i, g := range decoded {
		if len(g) != 1 {
			t.Errorf("group entry %d has %d keys, want exactly 1: %v", i, len(g), g)
		}
		for groupName, services := range g {
			want := len(groups[i].Services)
			if len(services) != want {
				t.Errorf("group %q decoded %d services, want %d", groupName, len(services), want)
			}
			for j, svc := range services {
				if len(svc) != 1 {
					t.Errorf("group %q service entry %d has %d keys, want exactly 1: %v", groupName, j, len(svc), svc)
				}
			}
		}
	}
}

func TestRenderer_RenderEntry(t *testing.T) {
	t.Parallel()

	svc := model.Service{
		Name:        "Jellyfin",
		URL:         "http://10.0.0.5:8096",
		Icon:        "jellyfin",
		Description: "Movies & TV",
	}

	got, err := New().RenderEntry(svc)
	if err != nil {
		t.Fatalf("RenderEntry() error = %v, want nil", err)
	}

	want := "Jellyfin:\n  description: Movies & TV\n  href: http://10.0.0.5:8096\n  icon: jellyfin\n"
	if string(got) != want {
		t.Errorf("RenderEntry() = %q, want %q", got, want)
	}
}

func TestRenderer_RenderEntry_MatchesRenderForTheSameService(t *testing.T) {
	t.Parallel()

	// internal/merge compares RenderEntry's output against what's already
	// in a file byte-for-byte; the two entry points had better agree on
	// what one service looks like, or that comparison is meaningless.
	svc := model.Service{Name: "Jellyfin", Group: "Media", URL: "http://x", Icon: "jellyfin"}

	entryOut, err := New().RenderEntry(svc)
	if err != nil {
		t.Fatalf("RenderEntry() error = %v, want nil", err)
	}

	docOut, err := New().Render([]model.Group{{Name: "Media", Services: []model.Service{svc}}})
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	var decoded []map[string][]map[string]any
	if err := yaml.Unmarshal(docOut, &decoded); err != nil {
		t.Fatalf("Render() output did not parse: %v", err)
	}
	var entryDecoded map[string]any
	if err := yaml.Unmarshal(entryOut, &entryDecoded); err != nil {
		t.Fatalf("RenderEntry() output did not parse: %v", err)
	}

	got := decoded[0]["Media"][0]
	if len(got) != 1 || len(entryDecoded) != 1 {
		t.Fatalf("expected single-key maps, got %v and %v", got, entryDecoded)
	}
	for name, fields := range got {
		wantFields, ok := entryDecoded[name]
		if !ok {
			t.Fatalf("Render() named the service %q, RenderEntry() didn't: %v", name, entryDecoded)
		}
		if a, b := fmt.Sprint(fields), fmt.Sprint(wantFields); a != b {
			t.Errorf("field mismatch for %q: Render()=%s RenderEntry()=%s", name, a, b)
		}
	}
}

func TestRenderer_NormalizeEntry_MatchesRenderEntryForUntouchedContent(t *testing.T) {
	t.Parallel()

	svc := model.Service{Name: "Jellyfin", URL: "http://10.0.0.5:8096", Icon: "jellyfin"}

	rendered, err := New().RenderEntry(svc)
	if err != nil {
		t.Fatalf("RenderEntry() error = %v, want nil", err)
	}

	// Embed the rendered entry several levels deep, exactly like it'd sit
	// inside a real services.yaml — this is the scenario NormalizeEntry
	// exists for: a node's own String() reflects the column it was parsed
	// at, which a byte comparison against RenderEntry's fresh,
	// top-level-column output must not be sensitive to.
	var nestedText strings.Builder
	nestedText.WriteString("- Media:\n")
	for i, line := range strings.Split(strings.TrimSuffix(string(rendered), "\n"), "\n") {
		if i == 0 {
			nestedText.WriteString("    - " + line + "\n") // the sequence marker for this entry
		} else {
			nestedText.WriteString("      " + line + "\n") // 6 more + rendered's own 2 = 8, matching a real file
		}
	}

	f, err := parser.ParseBytes([]byte(nestedText.String()), parser.ParseComments)
	if err != nil {
		t.Fatalf("parse nested fixture: %v\n%s", err, nestedText.String())
	}
	//nolint:forcetypeassert,errcheck // shape is controlled by this test's own fixture above
	root := f.Docs[0].Body.(*ast.SequenceNode)
	//nolint:forcetypeassert,errcheck
	mediaGroup := root.Values[0].(*ast.MappingNode)
	//nolint:forcetypeassert,errcheck
	servicesSeq := mediaGroup.Values[0].Value.(*ast.SequenceNode)
	entryNode := servicesSeq.Values[0]

	normalized, err := New().NormalizeEntry(entryNode)
	if err != nil {
		t.Fatalf("NormalizeEntry() error = %v, want nil", err)
	}
	if string(normalized) != string(rendered) {
		t.Errorf("NormalizeEntry() = %q, want it to match RenderEntry()'s canonical form %q", normalized, rendered)
	}
}

func TestRenderer_NormalizeEntry_MatchesRenderEntryForFlatExtraFields(t *testing.T) {
	t.Parallel()

	// The one existing round-trip test above never exercises a Service
	// with Extra set, so it never proves flatExtraFields' string values
	// survive internal/merge's actual comparison path: RenderEntry writes
	// them, but hand-edit detection re-reads them through NormalizeEntry
	// (parse -> NodeToValue -> re-marshal) on every later sync. "true" is
	// deliberately used here, not a normal word: it's the one value most
	// likely to round-trip as the YAML bool `true` instead of the string
	// "true" dashsync actually wrote, which would make NormalizeEntry's
	// output diverge from RenderEntry's and every sync after the first
	// see a false hand-edit conflict on a field nobody touched.
	svc := model.Service{
		Name: "Chainle",
		URL:  "https://chainle.ru",
		Extra: map[string]string{
			"homepage.server":      "my-docker",
			"homepage.container":   "chainleru-web-1",
			"homepage.showStats":   "true",
			"homepage.widget.type": "jellyfin",
		},
	}

	rendered, err := New().RenderEntry(svc)
	if err != nil {
		t.Fatalf("RenderEntry() error = %v, want nil", err)
	}

	var nestedText strings.Builder
	nestedText.WriteString("- Websites:\n")
	for i, line := range strings.Split(strings.TrimSuffix(string(rendered), "\n"), "\n") {
		if i == 0 {
			nestedText.WriteString("    - " + line + "\n")
		} else {
			nestedText.WriteString("      " + line + "\n")
		}
	}

	f, err := parser.ParseBytes([]byte(nestedText.String()), parser.ParseComments)
	if err != nil {
		t.Fatalf("parse nested fixture: %v\n%s", err, nestedText.String())
	}
	//nolint:forcetypeassert,errcheck // shape is controlled by this test's own fixture above
	root := f.Docs[0].Body.(*ast.SequenceNode)
	//nolint:forcetypeassert,errcheck
	group := root.Values[0].(*ast.MappingNode)
	//nolint:forcetypeassert,errcheck
	servicesSeq := group.Values[0].Value.(*ast.SequenceNode)
	entryNode := servicesSeq.Values[0]

	normalized, err := New().NormalizeEntry(entryNode)
	if err != nil {
		t.Fatalf("NormalizeEntry() error = %v, want nil", err)
	}
	if string(normalized) != string(rendered) {
		t.Errorf("NormalizeEntry() = %q, want it to match RenderEntry()'s canonical form %q\n(a mismatch here means a sync would flag this entry as hand-edited forever, even with nothing touched)",
			normalized, rendered)
	}
}

func TestRenderer_Render_NoGroups(t *testing.T) {
	t.Parallel()

	got, err := New().Render(nil)
	if err != nil {
		t.Fatalf("Render(nil) error = %v, want nil", err)
	}
	if string(got) != "[]\n" {
		t.Errorf("Render(nil) = %q, want the empty-sequence form %q", got, "[]\n")
	}
}

// assertGolden renders groups and compares the result against
// testdata/name, byte for byte. Run `go test ./internal/render/homepage/... -update`
// to write/refresh the golden file after an intentional output change.
func assertGolden(t *testing.T, name string, groups []model.Group) {
	t.Helper()

	got, err := New().Render(groups)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("write golden file %s: %v", path, err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v (run with -update if it doesn't exist yet)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("Render() does not match %s (run with -update if this change is intentional)\ngot:\n%s\nwant:\n%s",
			path, got, want)
	}
}
