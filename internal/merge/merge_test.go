package merge_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/NikitaMikhailov/dashsync/internal/merge"
	"github.com/NikitaMikhailov/dashsync/internal/model"
	"github.com/NikitaMikhailov/dashsync/internal/render/homepage"
)

func jellyfin(url string) model.Service {
	return model.Service{ID: "id1", Name: "Jellyfin", Group: "Media", URL: url}
}

// seed runs an ordinary Merge from nothing to produce a realistic
// "existing file" fixture, markers and all — hand-typing a marker's
// content hash into a test fixture is exactly the kind of detail that
// silently rots the moment the hashing or rendering logic changes at all,
// which is worth learning from a real mistake rather than a warning:
// several of these tests originally hardcoded a guessed hash, and every
// one of those guesses was wrong.
func seed(t *testing.T, services ...model.Service) []byte {
	t.Helper()

	out, _, err := merge.Merge(nil, model.GroupServices(services), homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("seed: Merge() error = %v, want nil", err)
	}
	return out
}

func TestMerge_NoExistingFile(t *testing.T) {
	t.Parallel()

	for _, existing := range [][]byte{nil, []byte(""), []byte("   \n")} {
		out, changes, err := merge.Merge(existing, model.GroupServices([]model.Service{jellyfin("http://x")}), homepage.New(), merge.Options{})
		if err != nil {
			t.Fatalf("Merge(%q) error = %v, want nil", existing, err)
		}
		if !strings.Contains(string(out), "- Media:") || !strings.Contains(string(out), "href: http://x") {
			t.Errorf("Merge(%q) = %q, want a fresh Media group containing Jellyfin", existing, out)
		}
		if len(changes) != 1 || changes[0].Kind != merge.Added {
			t.Errorf("Merge(%q) changes = %+v, want a single Added change", existing, changes)
		}
	}
}

func TestMerge_AddsNewServiceToExistingGroup(t *testing.T) {
	t.Parallel()

	existing := []byte(`- Media:
    - Manual Entry:
        href: https://example.com
`)

	out, changes, err := merge.Merge(existing, model.GroupServices([]model.Service{jellyfin("http://x")}), homepage.New(), merge.Options{})
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
}

func TestMerge_Idempotent(t *testing.T) {
	t.Parallel()

	existing := []byte(`- Media:
    - Manual Entry:
        href: https://example.com
`)
	groups := model.GroupServices([]model.Service{jellyfin("http://x")})

	out1, _, err := merge.Merge(existing, groups, homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("first Merge() error = %v, want nil", err)
	}

	out2, changes2, err := merge.Merge(out1, groups, homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("second Merge() error = %v, want nil", err)
	}

	if string(out1) != string(out2) {
		t.Errorf("two runs against unchanged input produced different output:\nrun 1:\n%s\nrun 2:\n%s", out1, out2)
	}
	if len(changes2) != 0 {
		t.Errorf("second run changes = %+v, want none (nothing changed)", changes2)
	}
}

func TestMerge_UpdatesChangedField(t *testing.T) {
	t.Parallel()

	existing := seed(t, jellyfin("http://x"))

	out, changes, err := merge.Merge(existing, model.GroupServices([]model.Service{jellyfin("http://y")}), homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("Merge() error = %v, want nil", err)
	}
	if !strings.Contains(string(out), "href: http://y") {
		t.Errorf("URL was not updated:\n%s", out)
	}
	if len(changes) != 1 || changes[0].Kind != merge.Updated {
		t.Errorf("changes = %+v, want a single Updated change", changes)
	}
}

func TestMerge_ServiceRenamed(t *testing.T) {
	t.Parallel()

	// Same ID (same container), display name changed via the
	// dashsync.name label — Merge must recognize this as the same
	// service (by ID) and update it in place, not add a duplicate.
	existing := seed(t, jellyfin("http://x"))
	renamed := model.Service{ID: "id1", Name: "Jellyfin (renamed)", Group: "Media", URL: "http://x"}

	out, changes, err := merge.Merge(existing, model.GroupServices([]model.Service{renamed}), homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("Merge() error = %v, want nil", err)
	}
	if !strings.Contains(string(out), "Jellyfin (renamed):") {
		t.Errorf("service was not renamed:\n%s", out)
	}
	if len(changes) != 1 || changes[0].Kind != merge.Updated {
		t.Errorf("changes = %+v, want a single Updated change (rename is just another field update)", changes)
	}
}

func TestMerge_ServiceMovedGroups(t *testing.T) {
	t.Parallel()

	// The dashsync.group label changed: the service should be removed
	// from its old group and appended fresh to the new one.
	existing := seed(t, jellyfin("http://x"))
	moved := model.Service{ID: "id1", Name: "Jellyfin", Group: "Entertainment", URL: "http://x"}

	out, changes, err := merge.Merge(existing, model.GroupServices([]model.Service{moved}), homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("Merge() error = %v, want nil", err)
	}

	want := "- Media: []\n- Entertainment:\n"
	if !strings.HasPrefix(string(out), want) || !strings.Contains(string(out), "href: http://x") {
		t.Errorf("Merge() =\n%q\nwant it to start with %q and still contain the service", out, want)
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
}

func TestMerge_RemovesDisappearedService(t *testing.T) {
	t.Parallel()

	manualOnly := []byte(`- Media:
    - Manual Entry:
        href: https://example.com
`)
	existing, _, err := merge.Merge(manualOnly, model.GroupServices([]model.Service{jellyfin("http://x")}), homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("seed: Merge() error = %v, want nil", err)
	}

	out, changes, err := merge.Merge(existing, nil, homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("Merge() error = %v, want nil", err)
	}

	want := "- Media:\n    - Manual Entry:\n        href: https://example.com\n"
	if string(out) != want {
		t.Errorf("Merge() =\n%q\nwant:\n%q", out, want)
	}
	if len(changes) != 1 || changes[0].Kind != merge.Removed || changes[0].ServiceID != "id1" {
		t.Errorf("changes = %+v, want a single Removed change for id1", changes)
	}
}

func TestMerge_GroupBecomesEmpty(t *testing.T) {
	t.Parallel()

	existing := seed(t, jellyfin("http://x"))

	out, _, err := merge.Merge(existing, nil, homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("Merge() error = %v, want nil", err)
	}
	if string(out) != "- Media: []\n" {
		t.Errorf("Merge() = %q, want an empty (but still valid, still a list) group", out)
	}
}

func TestMerge_RepopulateEmptiedGroup(t *testing.T) {
	t.Parallel()

	services := model.GroupServices([]model.Service{jellyfin("http://x")})

	out, changes, err := merge.Merge([]byte("- Media: []\n"), services, homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("Merge() error = %v, want nil", err)
	}
	if !strings.Contains(string(out), "- Media:\n    # dashsync:managed") || !strings.Contains(string(out), "href: http://x") {
		t.Errorf("Merge() = %q, want a repopulated, correctly indented Media group", out)
	}
	if len(changes) != 1 || changes[0].Kind != merge.Added {
		t.Errorf("changes = %+v, want a single Added change", changes)
	}

	// And the result must itself be stable.
	out2, changes2, err := merge.Merge(out, services, homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("second Merge() error = %v, want nil", err)
	}
	if string(out) != string(out2) || len(changes2) != 0 {
		t.Errorf("repopulated group is not stable: changes=%+v identical=%v", changes2, string(out) == string(out2))
	}
}

func TestMerge_ExistingFlowStyleGroupGetsBlockStyleIndentation(t *testing.T) {
	t.Parallel()

	// A group hand-collapsed to flow style (a human doing this via an
	// editor's YAML formatter is entirely plausible) has the same stale
	// column problem an empty flow-style group does — see
	// fixFlowStyleColumn's doc comment. Gaining a new managed entry must
	// still produce clean, correctly indented block-style output, not the
	// wildly over-indented result this test caught during development.
	existing := []byte("- Media: [Manual Entry: {href: https://example.com}]\n")

	out, _, err := merge.Merge(existing, model.GroupServices([]model.Service{jellyfin("http://x")}), homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("Merge() error = %v, want nil", err)
	}

	want := "- Media:\n    - Manual Entry: {href: https://example.com}\n    # dashsync:managed id=id1 content=6d9dc9df\n    - Jellyfin:\n        href: http://x\n"
	if string(out) != want {
		t.Errorf("Merge() =\n%q\nwant:\n%q", out, want)
	}
}

func TestMerge_NewGroupAppendedAtEnd(t *testing.T) {
	t.Parallel()

	existing := []byte(`- Network:
    - Manual Network Thing:
        href: http://z
`)
	services := []model.Service{
		jellyfin("http://x"), // Group: "Media" — doesn't exist in the file yet
	}

	out, changes, err := merge.Merge(existing, model.GroupServices(services), homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("Merge() error = %v, want nil", err)
	}

	networkIdx := strings.Index(string(out), "- Network:")
	mediaIdx := strings.Index(string(out), "- Media:")
	if networkIdx < 0 || mediaIdx < 0 || networkIdx > mediaIdx {
		t.Errorf("Merge() =\n%s\nwant the existing Network group to stay before the newly appended Media group", out)
	}
	if !strings.Contains(string(out), "Manual Network Thing") {
		t.Error("hand-written entry in the existing group was lost")
	}
	if len(changes) != 1 || changes[0].Kind != merge.Added {
		t.Errorf("changes = %+v, want a single Added change", changes)
	}
}

func TestMerge_ForeignCommentIsNotMistakenForAMarker(t *testing.T) {
	t.Parallel()

	// A human's own comment on their own entry must never be treated as
	// dashsync's marker — that would make Merge think it owns (and can
	// remove) an entry it never wrote.
	existing := []byte(`- Media:
    # just a note to self, nothing to do with dashsync
    - Manual Entry:
        href: https://example.com
`)

	out, changes, err := merge.Merge(existing, nil, homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("Merge() error = %v, want nil", err)
	}
	if string(out) != string(existing) {
		t.Errorf("an entry with a foreign comment was modified:\ngot:\n%s\nwant (unchanged):\n%s", out, existing)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %+v, want none — nothing here is dashsync's to remove", changes)
	}
}

func TestMerge_HandEditConflict(t *testing.T) {
	t.Parallel()

	clean := seed(t, jellyfin("http://x"))
	// dashsync never wrote "HAND-EDITED" — swap in a value it doesn't
	// know about, keeping the same (now stale) marker.
	handEdited := []byte(strings.Replace(string(clean), "href: http://x", "href: http://HAND-EDITED", 1))
	services := model.GroupServices([]model.Service{jellyfin("http://x")})

	t.Run("preserve (default) leaves it untouched and reports a conflict", func(t *testing.T) {
		t.Parallel()

		out, changes, err := merge.Merge(handEdited, services, homepage.New(), merge.Options{})
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

		out, changes, err := merge.Merge(handEdited, services, homepage.New(), merge.Options{Conflict: merge.Overwrite})
		if err != nil {
			t.Fatalf("Merge() error = %v, want nil", err)
		}
		if !strings.Contains(string(out), "href: http://x") || strings.Contains(string(out), "HAND-EDITED") {
			t.Errorf("Overwrite did not replace the hand-edited content:\n%s", out)
		}
		if len(changes) != 1 || changes[0].Kind != merge.Updated {
			t.Errorf("changes = %+v, want a single Updated change", changes)
		}
	})

	t.Run("fail aborts with a ConflictError and touches nothing", func(t *testing.T) {
		t.Parallel()

		out, changes, err := merge.Merge(handEdited, services, homepage.New(), merge.Options{Conflict: merge.Fail})
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
}

func TestMerge_HandEditedOrphanIsRemovedAnyway(t *testing.T) {
	t.Parallel()

	// The service is both hand-edited AND gone from Docker. Preserve only
	// protects a hand-edited entry from being overwritten with fresh
	// content — it was never a promise to keep an orphan around forever.
	clean := seed(t, jellyfin("http://x"))
	handEditedAndGone := []byte(strings.Replace(string(clean), "href: http://x", "href: http://HAND-EDITED", 1))

	out, changes, err := merge.Merge(handEditedAndGone, nil, homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("Merge() error = %v, want nil", err)
	}
	if string(out) != "- Media: []\n" {
		t.Errorf("Merge() = %q, want the orphaned hand-edited entry removed regardless", out)
	}
	if len(changes) != 1 || changes[0].Kind != merge.Removed {
		t.Errorf("changes = %+v, want a single Removed change", changes)
	}
}

func TestMerge_HandAddedUnmodeledFieldIsDetectedAsAConflict(t *testing.T) {
	t.Parallel()

	// Homepage supports fields dashsync's model has no place for
	// (siteMonitor, ping, container, ...). A human adding one of these
	// directly to a managed entry must be detected as a hand edit — not
	// silently dropped the next time an unrelated field (like href)
	// legitimately changes upstream. This is exactly the failure mode
	// NormalizeEntry decoding into a generic map (not a fixed struct) is
	// there to prevent.
	clean := seed(t, jellyfin("http://x"))
	withExtraField := []byte(strings.Replace(string(clean),
		"href: http://x\n", "href: http://x\n        siteMonitor: http://x/health\n", 1))

	updated := jellyfin("http://y") // an ordinary, unrelated upstream change

	out, changes, err := merge.Merge(withExtraField, model.GroupServices([]model.Service{updated}), homepage.New(), merge.Options{})
	if err != nil {
		t.Fatalf("Merge() error = %v, want nil", err)
	}

	if !strings.Contains(string(out), "siteMonitor: http://x/health") {
		t.Errorf("a field dashsync doesn't model was silently dropped instead of protected:\n%s", out)
	}
	if strings.Contains(string(out), "href: http://y") {
		t.Error("Preserve should mean the entry isn't touched at all, including the field dashsync does track")
	}
	if len(changes) != 1 || changes[0].Kind != merge.Conflict {
		t.Errorf("changes = %+v, want a single Conflict — a field dashsync doesn't model still counts as a hand edit", changes)
	}
}

func TestMerge_CorruptYAML(t *testing.T) {
	t.Parallel()

	_, _, err := merge.Merge([]byte("- Media:\n    - [unterminated"), nil, homepage.New(), merge.Options{})
	if err == nil {
		t.Fatal("Merge() error = nil, want a parse error for malformed YAML")
	}
}

func TestMerge_MalformedGroupValue(t *testing.T) {
	t.Parallel()

	// "Media" exists but its value is a string, not a list of services.
	existing := []byte("- Media: not a list\n")

	_, _, err := merge.Merge(existing, model.GroupServices([]model.Service{jellyfin("http://x")}), homepage.New(), merge.Options{})
	if err == nil {
		t.Fatal("Merge() error = nil, want an error — dashsync can't merge services into a non-list group value")
	}
}

func TestChangeKind_String(t *testing.T) {
	t.Parallel()

	tests := map[merge.ChangeKind]string{
		merge.Added:    "added",
		merge.Updated:  "updated",
		merge.Removed:  "removed",
		merge.Conflict: "conflict",
	}
	for kind, want := range tests {
		if got := kind.String(); got != want {
			t.Errorf("ChangeKind(%d).String() = %q, want %q", kind, got, want)
		}
	}
}
