package model

import "testing"

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
