package dashy

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

var update = flag.Bool("update", false, "update golden files")

func TestRenderer_Name(t *testing.T) {
	t.Parallel()

	if got := New().Name(); got != "dashy" {
		t.Errorf("Name() = %q, want %q", got, "dashy")
	}
}

func TestRenderer_DefaultPath(t *testing.T) {
	t.Parallel()

	if got := New().DefaultPath(); got != "conf.yml" {
		t.Errorf("DefaultPath() = %q, want %q", got, "conf.yml")
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

	assertGolden(t, "sections.golden.yaml", groups)
}

func TestRenderer_Render_EmptyService(t *testing.T) {
	t.Parallel()

	// Only "title" is required — no URL, icon, or description at all.
	groups := []model.Group{
		{Name: "Other", Services: []model.Service{{Name: "internal-svc"}}},
	}

	assertGolden(t, "empty_fields.golden.yaml", groups)
}

func TestRenderer_Render_ExtraFields(t *testing.T) {
	t.Parallel()

	// Extra label keys land directly on the item, not nested under a
	// sub-key the way Homepage's widget fields are. Both fields used here
	// are genuinely scalar in Dashy's own schema — unlike "tags" (see
	// extraFields' doc comment), which this mechanism can only ever emit
	// as a flat string, never Dashy's real array form.
	groups := []model.Group{
		{
			Name: "Network",
			Services: []model.Service{
				{
					Name: "Pi-hole",
					URL:  "http://10.0.0.5:80/admin",
					Extra: map[string]string{
						"dashy.target":  "newtab",
						"dashy.id":      "pihole-admin",
						"homepage.icon": "should not leak in: wrong prefix",
					},
				},
			},
		},
	}

	assertGolden(t, "extra_fields.golden.yaml", groups)
}

func TestRenderer_Render_GroupWithNoServices(t *testing.T) {
	t.Parallel()

	groups := []model.Group{{Name: "Empty"}}

	out, err := New().Render(groups)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	var decoded struct {
		Sections []struct {
			Name  string `yaml:"name"`
			Items []item `yaml:"items"`
		} `yaml:"sections"`
	}
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output did not parse: %v\noutput:\n%s", err, out)
	}
	if len(decoded.Sections) != 1 || decoded.Sections[0].Name != "Empty" {
		t.Fatalf("decoded = %+v, want one section named Empty", decoded)
	}
	if len(decoded.Sections[0].Items) != 0 {
		t.Errorf("items = %+v, want none", decoded.Sections[0].Items)
	}
}

func TestRenderer_Render_LabelOverridesTitle(t *testing.T) {
	t.Parallel()

	// itemFields sets "title" unconditionally before applying
	// extraFields — documented as label-wins-if-present, same as Homer's
	// "name" passthrough. Prove it actually happens rather than just
	// claiming it: this is the one field Dashy's shape actually diverges
	// from Homer's on, so it's the test most likely to catch a
	// copy-paste-from-Homer mistake.
	groups := []model.Group{
		{
			Name: "Media",
			Services: []model.Service{
				{Name: "Jellyfin", Extra: map[string]string{"dashy.title": "Overridden"}},
			},
		},
	}

	out, err := New().Render(groups)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	var decoded struct {
		Sections []struct {
			Items []map[string]any `yaml:"items"`
		} `yaml:"sections"`
	}
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output did not parse: %v\noutput:\n%s", err, out)
	}
	if len(decoded.Sections) != 1 || len(decoded.Sections[0].Items) != 1 {
		t.Fatalf("decoded = %+v, want one section with one item", decoded)
	}
	if got := decoded.Sections[0].Items[0]["title"]; got != "Overridden" {
		t.Errorf("item title = %v, want the dashsync.dashy.title label to win", got)
	}
}

func TestRenderer_Render_DashsyncDashyNameIsASilentNoOp(t *testing.T) {
	t.Parallel()

	// dashsync.dashy.name is NOT the identity override — dashsync.dashy.title
	// is. Given how visually similar Homer's and Dashy's label prefixes
	// are (differing only in which suffix renames an item), a user
	// following Homer's convention out of habit might reasonably set this
	// expecting a rename. It isn't an error — any unmodeled key just
	// passes through — but it silently does nothing useful, which is
	// worth pinning down explicitly rather than leaving as an
	// undocumented surprise.
	groups := []model.Group{
		{
			Name: "Media",
			Services: []model.Service{
				{Name: "Jellyfin", Extra: map[string]string{"dashy.name": "Ignored"}},
			},
		},
	}

	out, err := New().Render(groups)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	var decoded struct {
		Sections []struct {
			Items []map[string]any `yaml:"items"`
		} `yaml:"sections"`
	}
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output did not parse: %v\noutput:\n%s", err, out)
	}
	item := decoded.Sections[0].Items[0]
	if item["title"] != "Jellyfin" {
		t.Errorf(`item["title"] = %v, want it unaffected by dashsync.dashy.name`, item["title"])
	}
	if item["name"] != "Ignored" {
		t.Errorf(`item["name"] = %v, want the label to pass through verbatim as an inert field`, item["name"])
	}
}

func TestRenderer_Render_YAMLSignificantValuesStayLiteralStrings(t *testing.T) {
	t.Parallel()

	// A Docker label's value is effectively user-controlled. A description
	// or extra field that happens to look like a YAML bool, number, or
	// block-sequence marker must round-trip as the exact string it was,
	// not get reinterpreted by whatever parses conf.yml next. "dashy.tags"
	// here doubles as a reminder that this mechanism never turns the value
	// into Dashy's real array-valued `tags` form (see extraFields' doc
	// comment) — the literal-string requirement below is exactly why it
	// can't: a value that started as "a, b" must stay that one string, not
	// become ["a", "b"].
	groups := []model.Group{
		{
			Name: "Media",
			Services: []model.Service{
				{
					Name:        "Jellyfin",
					Description: "yes",
					Extra: map[string]string{
						"dashy.tags":   "- not a list item",
						"dashy.status": "42",
					},
				},
			},
		},
	}

	out, err := New().Render(groups)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	var decoded struct {
		Sections []struct {
			Items []map[string]any `yaml:"items"`
		} `yaml:"sections"`
	}
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output did not parse: %v\noutput:\n%s", err, out)
	}
	item := decoded.Sections[0].Items[0]

	want := map[string]any{
		"title":       "Jellyfin",
		"description": "yes",
		"tags":        "- not a list item",
		"status":      "42",
	}
	for key, wantVal := range want {
		if got := item[key]; got != wantVal {
			t.Errorf("item[%q] = %v (%T), want %v (%T) as a literal string", key, got, got, wantVal, wantVal)
		}
	}
}

func TestRenderer_Render_NoGroups(t *testing.T) {
	t.Parallel()

	got, err := New().Render(nil)
	if err != nil {
		t.Fatalf("Render(nil) error = %v, want nil", err)
	}
	want := "sections: []\n"
	if string(got) != want {
		t.Errorf("Render(nil) = %q, want %q", got, want)
	}
}

func TestRenderer_Render_GroupIdentifiedByNameFieldItemByTitleField(t *testing.T) {
	t.Parallel()

	// A section is identified by "name", but an item within it is
	// identified by "title" — not "name", unlike Homer's otherwise
	// identical shape. Assert both fields explicitly so a copy-paste
	// mistake from homer.go (leaving an item keyed by "name") fails here
	// rather than silently producing a Dashy config with unidentifiable
	// items.
	groups := []model.Group{
		{Name: "Media", Services: []model.Service{{Name: "Jellyfin", URL: "http://x"}}},
	}

	out, err := New().Render(groups)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	var decoded struct {
		Sections []struct {
			Name  string           `yaml:"name"`
			Items []map[string]any `yaml:"items"`
		} `yaml:"sections"`
	}
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output did not parse: %v\noutput:\n%s", err, out)
	}

	if len(decoded.Sections) != 1 || decoded.Sections[0].Name != "Media" {
		t.Fatalf("decoded = %+v, want one section named Media", decoded)
	}
	if len(decoded.Sections[0].Items) != 1 {
		t.Fatalf("decoded items = %+v, want exactly one", decoded.Sections[0].Items)
	}
	item := decoded.Sections[0].Items[0]
	if item["title"] != "Jellyfin" {
		t.Errorf("item[\"title\"] = %v, want %q", item["title"], "Jellyfin")
	}
	if _, hasName := item["name"]; hasName {
		t.Errorf("item has a \"name\" field = %v, want none — Dashy identifies items by \"title\"", item["name"])
	}
}

// assertGolden renders groups and compares the result against
// testdata/name, byte for byte. Run `go test ./internal/render/dashy/... -update`
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
