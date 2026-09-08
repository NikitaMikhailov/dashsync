package homer

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

	if got := New().Name(); got != "homer" {
		t.Errorf("Name() = %q, want %q", got, "homer")
	}
}

func TestRenderer_DefaultPath(t *testing.T) {
	t.Parallel()

	if got := New().DefaultPath(); got != "config.yml" {
		t.Errorf("DefaultPath() = %q, want %q", got, "config.yml")
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

	// Only "name" is required — no URL, icon, or description at all.
	groups := []model.Group{
		{Name: "Other", Services: []model.Service{{Name: "internal-svc"}}},
	}

	assertGolden(t, "empty_fields.golden.yaml", groups)
}

func TestRenderer_Render_ExtraFields(t *testing.T) {
	t.Parallel()

	// Extra label keys land directly on the item, not nested under a
	// sub-key the way Homepage's widget fields are.
	groups := []model.Group{
		{
			Name: "Network",
			Services: []model.Service{
				{
					Name: "Pi-hole",
					URL:  "http://10.0.0.5:80/admin",
					Extra: map[string]string{
						"homer.type":    "PiHole",
						"homer.tag":     "dns",
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
		Services []struct {
			Name  string `yaml:"name"`
			Items []item `yaml:"items"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output did not parse: %v\noutput:\n%s", err, out)
	}
	if len(decoded.Services) != 1 || decoded.Services[0].Name != "Empty" {
		t.Fatalf("decoded = %+v, want one group named Empty", decoded)
	}
	if len(decoded.Services[0].Items) != 0 {
		t.Errorf("items = %+v, want none", decoded.Services[0].Items)
	}
}

func TestRenderer_Render_LabelOverridesName(t *testing.T) {
	t.Parallel()

	// itemFields sets "name" unconditionally before applying extraFields —
	// documented as label-wins-if-present, same as Homepage's widget
	// passthrough. Prove it actually happens rather than just claiming it.
	groups := []model.Group{
		{
			Name: "Media",
			Services: []model.Service{
				{Name: "Jellyfin", Extra: map[string]string{"homer.name": "Overridden"}},
			},
		},
	}

	out, err := New().Render(groups)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	var decoded struct {
		Services []struct {
			Items []map[string]any `yaml:"items"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output did not parse: %v\noutput:\n%s", err, out)
	}
	if len(decoded.Services) != 1 || len(decoded.Services[0].Items) != 1 {
		t.Fatalf("decoded = %+v, want one group with one item", decoded)
	}
	if got := decoded.Services[0].Items[0]["name"]; got != "Overridden" {
		t.Errorf("item name = %v, want the dashsync.homer.name label to win", got)
	}
}

func TestRenderer_Render_YAMLSignificantValuesStayLiteralStrings(t *testing.T) {
	t.Parallel()

	// A Docker label's value is effectively user-controlled. A description
	// or extra field that happens to look like a YAML bool, number, or
	// block-sequence marker must round-trip as the exact string it was,
	// not get reinterpreted by whatever parses config.yml next.
	groups := []model.Group{
		{
			Name: "Media",
			Services: []model.Service{
				{
					Name:        "Jellyfin",
					Description: "yes",
					Extra: map[string]string{
						"homer.tag":    "- not a list item",
						"homer.status": "42",
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
		Services []struct {
			Items []map[string]any `yaml:"items"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output did not parse: %v\noutput:\n%s", err, out)
	}
	item := decoded.Services[0].Items[0]

	want := map[string]any{
		"name":     "Jellyfin",
		"subtitle": "yes",
		"tag":      "- not a list item",
		"status":   "42",
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
	want := "services: []\n"
	if string(got) != want {
		t.Errorf("Render(nil) = %q, want %q", got, want)
	}
}

func TestRenderer_Render_GroupIdentifiedByNameField(t *testing.T) {
	t.Parallel()

	// Unlike Homepage, a group/item here is a regular multi-field object
	// with a "name" field — not a single-key map. Decode generically and
	// check the field exists, rather than assuming any particular map
	// shape.
	groups := []model.Group{
		{Name: "Media", Services: []model.Service{{Name: "Jellyfin", URL: "http://x"}}},
	}

	out, err := New().Render(groups)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	var decoded struct {
		Services []struct {
			Name  string           `yaml:"name"`
			Items []map[string]any `yaml:"items"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output did not parse: %v\noutput:\n%s", err, out)
	}

	if len(decoded.Services) != 1 || decoded.Services[0].Name != "Media" {
		t.Fatalf("decoded = %+v, want one group named Media", decoded)
	}
	if len(decoded.Services[0].Items) != 1 || decoded.Services[0].Items[0]["name"] != "Jellyfin" {
		t.Errorf("decoded items = %+v, want one item named Jellyfin", decoded.Services[0].Items)
	}
}

// assertGolden renders groups and compares the result against
// testdata/name, byte for byte. Run `go test ./internal/render/homer/... -update`
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
