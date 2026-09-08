package model

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestSource_ID(t *testing.T) {
	t.Parallel()

	a := Source{Host: "homelab-1", Container: "jellyfin"}
	b := Source{Host: "homelab-1", Container: "jellyfin"}
	c := Source{Host: "homelab-1", Container: "sonarr"}
	d := Source{Host: "homelab-2", Container: "jellyfin"}
	// Host is free-form config (M4); a naive Host+"/"+Container join would
	// collide here since both sides produce "a/b" and "c".
	e := Source{Host: "a/b", Container: "c"}
	f := Source{Host: "a", Container: "b/c"}

	if a.ID() != b.ID() {
		t.Errorf("same Source produced different IDs: %q vs %q", a.ID(), b.ID())
	}
	if a.ID() == c.ID() {
		t.Errorf("different containers on the same host got the same ID: %q", a.ID())
	}
	if a.ID() == d.ID() {
		t.Errorf("same container on different hosts got the same ID: %q", a.ID())
	}
	if e.ID() == f.ID() {
		t.Errorf("Host/Container boundary collision: {%q,%q} and {%q,%q} got the same ID %q",
			e.Host, e.Container, f.Host, f.Container, e.ID())
	}
	if got := a.ID(); len(got) != 12 {
		t.Errorf("ID() = %q, want 12 hex characters, got %d", got, len(got))
	}
}

func TestGroupServices(t *testing.T) {
	t.Parallel()

	// Deliberately scrambled: neither group order nor within-group order
	// matches what the output should be — GroupServices must not depend on
	// its input already being sorted.
	//
	// jellyfin/jellyfin (same Name, different ID) models two same-named
	// containers on two different hosts — completely normal in multi-host
	// setups (M4), since Docker only enforces name uniqueness within one
	// daemon. A true same-ID duplicate isn't modeled here: it would require
	// two identical (Host, Container) pairs, which Discover() can't
	// produce from a single ContainerList call, since Docker itself
	// guarantees container names are unique within one daemon.
	services := []Service{
		{ID: "2", Name: "sonarr", Group: "Media"},
		{ID: "1", Name: "adguard", Group: "Network"},
		{ID: "3", Name: "jellyfin", Group: "Media"},
		{ID: "4", Name: "jellyfin", Group: "Media"},
		{ID: "5", Name: "unlabeled-group"}, // Group left unset entirely
	}

	want := []Group{
		{
			Name: "Media",
			Services: []Service{
				{ID: "3", Name: "jellyfin", Group: "Media"},
				{ID: "4", Name: "jellyfin", Group: "Media"},
				{ID: "2", Name: "sonarr", Group: "Media"},
			},
		},
		{
			Name: "Network",
			Services: []Service{
				{ID: "1", Name: "adguard", Group: "Network"},
			},
		},
		{
			Name: "Other",
			Services: []Service{
				{ID: "5", Name: "unlabeled-group"},
			},
		},
	}

	got := GroupServices(services)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GroupServices() mismatch (-want +got):\n%s", diff)
	}
}
