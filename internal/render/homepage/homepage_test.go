package homepage

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
						"homepage.icon":        "should not leak into widget: wrong prefix",
					},
				},
			},
		},
	}

	assertGolden(t, "widget.golden.yaml", groups)
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
