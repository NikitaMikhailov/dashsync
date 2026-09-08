// Package model defines dashsync's canonical representation of a
// dashboard-worthy service. It deliberately doesn't mirror any single
// dashboard's config format — turning it into one is a renderer's job.
package model

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"slices"
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
	// doesn't silently look identical to a running one when read back with
	// `inspect`. No renderer consumes it yet: none of the dashboard
	// formats dashsync targets have a field for "this link is currently
	// dead," so as of M2 it's inspect-only. Revisit once a renderer wants
	// it — e.g. to skip stopped services, or annotate them somehow.
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

// Group is a named collection of services — the unit a Renderer works with.
type Group struct {
	Name     string
	Services []Service
}

// GroupServices buckets services by their Group field into Groups sorted by
// name, with each Group's own Services sorted by (Name, ID). It sorts
// unconditionally rather than trusting the input's order: determinism is a
// property of this function, not an assumption about what its caller
// already did.
//
// A Service with no Group set is bucketed under "Other" rather than into a
// nameless group. internal/discovery already defaults an unset
// dashsync.group label to "Other" before a Service exists at all, so this
// mostly guards against a Service built some other way (a test, or a
// future non-Docker discovery source) rather than anything the current
// pipeline can actually produce.
func GroupServices(services []Service) []Group {
	byGroup := make(map[string][]Service)
	for _, s := range services {
		name := s.Group
		if name == "" {
			name = "Other"
		}
		byGroup[name] = append(byGroup[name], s)
	}

	names := make([]string, 0, len(byGroup))
	for name := range byGroup {
		names = append(names, name)
	}
	slices.Sort(names)

	groups := make([]Group, 0, len(names))
	for _, name := range names {
		svcs := byGroup[name]
		slices.SortFunc(svcs, func(a, b Service) int {
			if c := cmp.Compare(a.Name, b.Name); c != 0 {
				return c
			}
			return cmp.Compare(a.ID, b.ID)
		})
		groups = append(groups, Group{Name: name, Services: svcs})
	}
	return groups
}
