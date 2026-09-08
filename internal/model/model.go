// Package model defines dashsync's canonical representation of a
// dashboard-worthy service. It deliberately doesn't mirror any single
// dashboard's config format — turning it into one is a renderer's job.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// Service is one thing to show on a dashboard.
type Service struct {
	// ID is stable across runs for the same Source: it's what managed
	// markers (M3) key off to recognize "this is the entry I generated
	// last time" without caring whether Name, Group, or anything else
	// changed since.
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
	URL   string `json:"url,omitempty"`
	Icon  string `json:"icon,omitempty"`
	// Status is the discovered container's own state — "running",
	// "exited", "paused", etc. It exists so a stopped-but-enabled service
	// doesn't silently look identical to a running one: dashsync surfaces
	// what it found, a renderer or the human reading `inspect` decides
	// what a dead link means.
	Status      string `json:"status"`
	Description string `json:"description,omitempty"`
	// Extra carries renderer-specific fields straight through from labels
	// (see internal/discovery's label contract), keyed by whatever comes
	// after "dashsync." once the known top-level keys are stripped out.
	// Values stay strings because that's all a Docker label ever is —
	// turning them into `any` here would just move the type assertion
	// into every renderer instead of removing it.
	Extra  map[string]string `json:"extra,omitempty"`
	Source Source            `json:"source"`
}

// Source records where a Service was discovered.
type Source struct {
	// Host is the logical name of the Docker endpoint it came from (see
	// dashsync.yaml once multi-host support lands in M4), not necessarily
	// a resolvable hostname.
	Host string `json:"host"`
	// Container is the container's own name, without the leading "/"
	// Docker's API adds.
	Container string `json:"container"`
}

// ID derives a Service ID from where it was discovered: same Host and
// Container always produce the same ID, so a service keeps its identity
// across runs even if every other field about it changes.
//
// Host and Container are length-prefixed before hashing rather than just
// joined with a separator: Container names can't contain "/", but Host is
// free-form user config (M4), so Host="a/b",Container="c" and
// Host="a",Container="b/c" would hash identically under plain
// concatenation. Prefixing each part with its own length makes the
// encoding unambiguous regardless of what characters either field
// contains.
func (s Source) ID() string {
	input := strconv.Itoa(len(s.Host)) + ":" + s.Host +
		strconv.Itoa(len(s.Container)) + ":" + s.Container
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])[:12]
}
