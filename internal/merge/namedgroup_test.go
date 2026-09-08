package merge_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/NikitaMikhailov/dashsync/internal/merge"
	"github.com/NikitaMikhailov/dashsync/internal/model"
	"github.com/NikitaMikhailov/dashsync/internal/render/dashy"
	"github.com/NikitaMikhailov/dashsync/internal/render/homer"
)

// namedGroupCase bundles one NamedGroupAdapter-based renderer with the
// vocabulary these tests need to build fixtures independent of which
// renderer is under test. Running the same scenarios against both Homer
// and Dashy is what actually proves NamedGroupAdapter serves two real
// consumers, not just one real one and a sketch — see
// docs/decisions/007-document-adapter.md.
type namedGroupCase struct {
	name        string
	renderer    merge.EntryRenderer
	topLevelKey string
	itemField   string // "name" for Homer, "title" for Dashy — the one real divergence
}

func namedGroupCases() []namedGroupCase {
	return []namedGroupCase{
		{name: "homer", renderer: homer.New(), topLevelKey: "services", itemField: "name"},
		{name: "dashy", renderer: dashy.New(), topLevelKey: "sections", itemField: "title"},
	}
}

// --- Adapter-specific scenarios (the genuinely new mechanics) ---

func TestNamedGroupAdapter_NoExistingFile(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, changes, err := merge.Merge(nil, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() error = %v, want nil", err)
			}
			if !strings.Contains(string(out), tc.topLevelKey+":") {
				t.Errorf("output = %q, want it to bootstrap under %q, not a bare list", out, tc.topLevelKey)
			}
			if !strings.Contains(string(out), "Jellyfin") {
				t.Errorf("output = %q, want Jellyfin", out)
			}
			if len(changes) != 1 || changes[0].Kind != merge.Added {
				t.Errorf("changes = %+v, want a single Added change", changes)
			}
		})
	}
}

func TestNamedGroupAdapter_TopLevelKeyAppendedAmongForeignSettings(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The real case this whole adapter exists for: unrelated
			// hand-configured settings that must survive byte-for-byte
			// while the managed key is appended fresh.
			existing := []byte("title: My Dashboard\ntheme: default\n")
			out, _, err := merge.Merge(existing, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() error = %v, want nil", err)
			}
			if !strings.HasPrefix(string(out), "title: My Dashboard\ntheme: default\n") {
				t.Errorf("output =\n%s\nwant the foreign settings preserved verbatim at the top", out)
			}
			if !strings.Contains(string(out), tc.topLevelKey+":") || !strings.Contains(string(out), "Jellyfin") {
				t.Errorf("output =\n%s\nwant a fresh %q key appended containing Jellyfin", out, tc.topLevelKey)
			}
		})
	}
}

func TestNamedGroupAdapter_GroupMissingItemsFieldGetsOneAppended(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// A hand-written group with no "items:" key at all yet.
			existing := []byte(tc.topLevelKey + ":\n  - name: Media\n")
			out, changes, err := merge.Merge(existing, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() error = %v, want nil", err)
			}
			if !strings.Contains(string(out), "Jellyfin") {
				t.Errorf("output =\n%s\nwant Jellyfin added into the hand-written Media group", out)
			}
			if len(changes) != 1 || changes[0].Kind != merge.Added {
				t.Errorf("changes = %+v, want a single Added change", changes)
			}
		})
	}
}

func TestNamedGroupAdapter_MalformedTopLevelValue(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			existing := []byte(tc.topLevelKey + ": not a list\n")
			_, _, err := merge.Merge(existing, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err == nil {
				t.Fatal("Merge() error = nil, want an error — the top-level value isn't a list of groups")
			}
		})
	}
}

func TestNamedGroupAdapter_MalformedItemsValue(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			existing := []byte(tc.topLevelKey + ":\n  - name: Media\n    items: not a list\n")
			_, _, err := merge.Merge(existing, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err == nil {
				t.Fatal("Merge() error = nil, want an error — items isn't a list")
			}
		})
	}
}

func TestNamedGroupAdapter_UnrecognizedGroupEntryIsLeftAlone(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// No "name" field at all — not this adapter's to manage, and
			// not something the removal pass should choke on.
			existing := []byte(tc.topLevelKey + ":\n  - icon: some-icon\n    items: []\n")
			out, changes, err := merge.Merge(existing, nil, tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() error = %v, want nil", err)
			}
			if string(out) != string(existing) {
				t.Errorf("output =\n%s\nwant the unrecognized entry left untouched:\n%s", out, existing)
			}
			if len(changes) != 0 {
				t.Errorf("changes = %+v, want none", changes)
			}
		})
	}
}

// --- Re-running Homepage's own scenarios against NamedGroupAdapter ---
//
// These prove internal/merge's marker/entry-level machinery
// (readEntries/writeEntries/applyDesired/removeOrphaned/marker.go) really
// is unaffected by which DocumentAdapter is in play, rather than just
// asserting that in a doc comment.

func TestNamedGroupAdapter_Idempotent(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			services := model.GroupServices([]model.Service{jellyfin("http://x")})
			out1, _, err := merge.Merge(nil, services, tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("first Merge() error = %v, want nil", err)
			}
			out2, changes2, err := merge.Merge(out1, services, tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("second Merge() error = %v, want nil", err)
			}
			if string(out1) != string(out2) {
				t.Errorf("two runs against unchanged input produced different output:\nrun 1:\n%s\nrun 2:\n%s", out1, out2)
			}
			if len(changes2) != 0 {
				t.Errorf("second run changes = %+v, want none", changes2)
			}
		})
	}
}

func TestNamedGroupAdapter_ServiceRenamed(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			existing, _, err := merge.Merge(nil, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("seed: Merge() error = %v, want nil", err)
			}
			renamed := model.Service{ID: "id1", Name: "Jellyfin (renamed)", Group: "Media", URL: "http://x"}

			out, changes, err := merge.Merge(existing, model.GroupServices([]model.Service{renamed}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() error = %v, want nil", err)
			}
			if !strings.Contains(string(out), "Jellyfin (renamed)") {
				t.Errorf("output = %q, want the renamed service", out)
			}
			if len(changes) != 1 || changes[0].Kind != merge.Updated {
				t.Errorf("changes = %+v, want a single Updated change (rename is just another field update)", changes)
			}
		})
	}
}

func TestNamedGroupAdapter_ServiceMovedGroups(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			existing, _, err := merge.Merge(nil, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("seed: Merge() error = %v, want nil", err)
			}
			moved := model.Service{ID: "id1", Name: "Jellyfin", Group: "Entertainment", URL: "http://x"}

			out, changes, err := merge.Merge(existing, model.GroupServices([]model.Service{moved}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() error = %v, want nil", err)
			}
			if !strings.Contains(string(out), "Entertainment") || !strings.Contains(string(out), "Jellyfin") {
				t.Errorf("output = %q, want the service present under Entertainment", out)
			}
			if len(changes) != 2 {
				t.Fatalf("changes = %+v, want exactly 2 (removed from Media, added to Entertainment)", changes)
			}
			var sawRemoved, sawAdded bool
			for _, c := range changes {
				switch {
				case c.Kind == merge.Removed && c.Group == "Media":
					sawRemoved = true
				case c.Kind == merge.Added && c.Group == "Entertainment":
					sawAdded = true
				}
			}
			if !sawRemoved || !sawAdded {
				t.Errorf("changes = %+v, want a Removed from Media and an Added to Entertainment", changes)
			}
		})
	}
}

func TestNamedGroupAdapter_GroupBecomesEmptyAndRepopulates(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			existing, _, err := merge.Merge(nil, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("seed: Merge() error = %v, want nil", err)
			}

			emptied, changes, err := merge.Merge(existing, nil, tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() (emptying) error = %v, want nil", err)
			}
			if strings.Contains(string(emptied), "Jellyfin") {
				t.Errorf("output = %q, want Jellyfin removed", emptied)
			}
			if len(changes) != 1 || changes[0].Kind != merge.Removed {
				t.Errorf("changes = %+v, want a single Removed change", changes)
			}

			services := model.GroupServices([]model.Service{jellyfin("http://x")})
			repopulated, changes2, err := merge.Merge(emptied, services, tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() (repopulate) error = %v, want nil", err)
			}
			if !strings.Contains(string(repopulated), "Jellyfin") {
				t.Errorf("output = %q, want Jellyfin re-added", repopulated)
			}
			if len(changes2) != 1 || changes2[0].Kind != merge.Added {
				t.Errorf("changes = %+v, want a single Added change", changes2)
			}

			again, changes3, err := merge.Merge(repopulated, services, tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() (stability check) error = %v, want nil", err)
			}
			if string(repopulated) != string(again) || len(changes3) != 0 {
				t.Errorf("repopulated group is not stable: changes=%+v identical=%v",
					changes3, string(repopulated) == string(again))
			}
		})
	}
}

func TestNamedGroupAdapter_ForeignCommentIsNotMistakenForAMarker(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			existing := []byte(tc.topLevelKey + ":\n  - name: Media\n    items:\n      # just a note to self, nothing to do with dashsync\n      - " +
				tc.itemField + ": Manual Entry\n        url: https://example.com\n")

			out, changes, err := merge.Merge(existing, nil, tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() error = %v, want nil", err)
			}
			if string(out) != string(existing) {
				t.Errorf("an entry with a foreign comment was modified:\ngot:\n%s\nwant (unchanged):\n%s", out, existing)
			}
			if len(changes) != 0 {
				t.Errorf("changes = %+v, want none — nothing here is dashsync's to remove", changes)
			}
		})
	}
}

func TestNamedGroupAdapter_HandEditConflict(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clean, _, err := merge.Merge(nil, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("seed: Merge() error = %v, want nil", err)
			}
			handEdited := []byte(strings.Replace(string(clean), "url: http://x", "url: http://HAND-EDITED", 1))
			services := model.GroupServices([]model.Service{jellyfin("http://x")})

			t.Run("preserve (default) leaves it untouched and reports a conflict", func(t *testing.T) {
				t.Parallel()

				out, changes, err := merge.Merge(handEdited, services, tc.renderer, merge.Options{})
				if err != nil {
					t.Fatalf("Merge() error = %v, want nil", err)
				}
				if string(out) != string(handEdited) {
					t.Errorf("Preserve modified a hand-edited entry:\ngot:\n%s\nwant (unchanged):\n%s", out, handEdited)
				}
				if len(changes) != 1 || changes[0].Kind != merge.Conflict {
					t.Errorf("changes = %+v, want a single Conflict change", changes)
				}
			})

			t.Run("overwrite replaces it with the desired content", func(t *testing.T) {
				t.Parallel()

				out, changes, err := merge.Merge(handEdited, services, tc.renderer, merge.Options{Conflict: merge.Overwrite})
				if err != nil {
					t.Fatalf("Merge() error = %v, want nil", err)
				}
				if !strings.Contains(string(out), "http://x") || strings.Contains(string(out), "HAND-EDITED") {
					t.Errorf("Overwrite did not replace the hand-edited content:\n%s", out)
				}
				if len(changes) != 1 || changes[0].Kind != merge.Updated {
					t.Errorf("changes = %+v, want a single Updated change", changes)
				}
			})

			t.Run("fail aborts with a ConflictError and touches nothing", func(t *testing.T) {
				t.Parallel()

				out, changes, err := merge.Merge(handEdited, services, tc.renderer, merge.Options{Conflict: merge.Fail})
				if out != nil || changes != nil {
					t.Errorf("Merge() = (%v, %v), want (nil, nil) on failure", out, changes)
				}
				var conflictErr *merge.ConflictError
				if !errors.As(err, &conflictErr) {
					t.Fatalf("Merge() error = %v, want a *merge.ConflictError", err)
				}
				if conflictErr.ServiceID != "id1" {
					t.Errorf("ConflictError.ServiceID = %q, want %q", conflictErr.ServiceID, "id1")
				}
			})
		})
	}
}

func TestNamedGroupAdapter_HandAddedUnmodeledFieldIsDetectedAsAConflict(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clean, _, err := merge.Merge(nil, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("seed: Merge() error = %v, want nil", err)
			}
			// Both renderers produce "        url: http://x" at this exact
			// indentation for a single-group, single-item document —
			// verified empirically before hardcoding it here, the same
			// discipline as merge_test.go's own hand-edit fixtures.
			withExtraField := []byte(strings.Replace(string(clean),
				"url: http://x\n", "url: http://x\n        statusCheck: true\n", 1))

			updated := model.Service{ID: "id1", Name: "Jellyfin", Group: "Media", URL: "http://y"}
			out, changes, err := merge.Merge(withExtraField, model.GroupServices([]model.Service{updated}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() error = %v, want nil", err)
			}

			if !strings.Contains(string(out), "statusCheck: true") {
				t.Errorf("a field dashsync doesn't model was silently dropped instead of protected:\n%s", out)
			}
			if strings.Contains(string(out), "url: http://y") {
				t.Error("Preserve should mean the entry isn't touched at all, including the field dashsync does track")
			}
			if len(changes) != 1 || changes[0].Kind != merge.Conflict {
				t.Errorf("changes = %+v, want a single Conflict — a field dashsync doesn't model still counts as a hand edit", changes)
			}
		})
	}
}

func TestNamedGroupAdapter_AddsNewServiceAlongsideExistingItem(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// An existing group whose items list already has real,
			// hand-written content — the "find, don't append" path
			// through getOrAppendField/findField for the items field
			// itself, as opposed to every other adapter test's
			// empty-or-missing-items starting point.
			existing := []byte(tc.topLevelKey + ":\n  - name: Media\n    items:\n      - " +
				tc.itemField + ": Manual Entry\n        url: https://example.com\n")

			out, changes, err := merge.Merge(existing, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() error = %v, want nil", err)
			}
			if !strings.Contains(string(out), "Manual Entry") {
				t.Errorf("hand-written entry was lost:\n%s", out)
			}
			if !strings.Contains(string(out), "Jellyfin") {
				t.Errorf("new service was not added:\n%s", out)
			}
			if len(changes) != 1 || changes[0].Kind != merge.Added || changes[0].ServiceID != "id1" {
				t.Errorf("changes = %+v, want a single Added change for id1", changes)
			}
		})
	}
}

func TestNamedGroupAdapter_RemovesDisappearedServiceButSiblingSurvives(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			manualOnly := []byte(tc.topLevelKey + ":\n  - name: Media\n    items:\n      - " +
				tc.itemField + ": Manual Entry\n        url: https://example.com\n")
			existing, _, err := merge.Merge(manualOnly, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("seed: Merge() error = %v, want nil", err)
			}

			// Ordinary in-place removal — not the sole-entry-in-the-group
			// case TestNamedGroupAdapter_GroupBecomesEmptyAndRepopulates
			// already covers, which goes through writeEntries' empty-
			// sequence-replacement special case instead of this one.
			out, changes, err := merge.Merge(existing, nil, tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() error = %v, want nil", err)
			}
			if !strings.Contains(string(out), "Manual Entry") {
				t.Errorf("hand-written sibling entry was lost:\n%s", out)
			}
			if strings.Contains(string(out), "Jellyfin") {
				t.Errorf("removed service is still present:\n%s", out)
			}
			if len(changes) != 1 || changes[0].Kind != merge.Removed || changes[0].ServiceID != "id1" {
				t.Errorf("changes = %+v, want a single Removed change for id1", changes)
			}
		})
	}
}

func TestNamedGroupAdapter_NewGroupAppendedToPopulatedList(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			existing := []byte(tc.topLevelKey + ":\n  - name: Network\n    items:\n      - " +
				tc.itemField + ": Manual Network Thing\n        url: http://z\n")
			services := []model.Service{jellyfin("http://x")} // Group: "Media" — doesn't exist in the file yet

			out, changes, err := merge.Merge(existing, model.GroupServices(services), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() error = %v, want nil", err)
			}

			networkIdx := strings.Index(string(out), "name: Network")
			mediaIdx := strings.Index(string(out), "name: Media")
			if networkIdx < 0 || mediaIdx < 0 || networkIdx > mediaIdx {
				t.Errorf("output =\n%s\nwant the existing Network group to stay before the newly appended Media group", out)
			}
			if !strings.Contains(string(out), "Manual Network Thing") {
				t.Error("hand-written entry in the existing group was lost")
			}
			if len(changes) != 1 || changes[0].Kind != merge.Added {
				t.Errorf("changes = %+v, want a single Added change", changes)
			}
		})
	}
}

func TestNamedGroupAdapter_HandEditedOrphanIsRemovedAnyway(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The service is both hand-edited AND gone from Docker.
			// Preserve only protects a hand-edited entry from being
			// overwritten with fresh content — it was never a promise to
			// keep an orphan around forever.
			clean, _, err := merge.Merge(nil, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("seed: Merge() error = %v, want nil", err)
			}
			handEditedAndGone := []byte(strings.Replace(string(clean), "url: http://x", "url: http://HAND-EDITED", 1))

			out, changes, err := merge.Merge(handEditedAndGone, nil, tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() error = %v, want nil", err)
			}
			if strings.Contains(string(out), "HAND-EDITED") {
				t.Errorf("output = %q, want the orphaned hand-edited entry removed regardless", out)
			}
			if len(changes) != 1 || changes[0].Kind != merge.Removed {
				t.Errorf("changes = %+v, want a single Removed change", changes)
			}
		})
	}
}

func TestNamedGroupAdapter_ExistingFlowStyleItemsListGetsBlockStyle(t *testing.T) {
	t.Parallel()

	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The items list itself (not the groups list, not the group
			// entry — both already covered in adapter_test.go) hand-
			// collapsed to flow style, gaining a new managed entry.
			existing := []byte(tc.topLevelKey + ":\n  - name: Media\n    items: [{" +
				tc.itemField + ": Manual Entry, url: https://example.com}]\n")

			out, _, err := merge.Merge(existing, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("Merge() error = %v, want nil", err)
			}
			if !strings.Contains(string(out), "Manual Entry") || !strings.Contains(string(out), "Jellyfin") {
				t.Errorf("output =\n%s\nwant both the hand-written and new entries present", out)
			}

			// The real symptom the original bug produced: invalid YAML
			// that fails to re-parse, breaking idempotency outright.
			out2, changes2, err := merge.Merge(out, model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err != nil {
				t.Fatalf("second Merge() (idempotency check) error = %v, want nil", err)
			}
			if string(out) != string(out2) || len(changes2) != 0 {
				t.Errorf("not idempotent: changes=%+v identical=%v", changes2, string(out) == string(out2))
			}
		})
	}
}

func TestNamedGroupAdapter_EmptyExistingFileOfTheWrongShapeIsRejected(t *testing.T) {
	t.Parallel()

	// The bootstrap-ambiguity fix: a literal "[]" on disk (a legitimate,
	// if degenerate, Homepage-shaped file — or simply the wrong path) must
	// not be silently reinterpreted as "no file yet" just because it
	// happens to parse to the same empty-sequence shape parseOrEmpty's own
	// placeholder has.
	for _, tc := range namedGroupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := merge.Merge([]byte("[]\n"), model.GroupServices([]model.Service{jellyfin("http://x")}), tc.renderer, merge.Options{})
			if err == nil {
				t.Fatal("Merge() error = nil, want an error — an existing empty sequence isn't this format's shape")
			}
		})
	}
}
