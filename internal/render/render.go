// Package render turns dashsync's canonical model into the config format a
// specific self-hosted dashboard understands. Each dashboard gets its own
// subpackage (internal/render/homepage, ...); this package only holds the
// shared contract between them and the CLI.
package render

import "github.com/NikitaMikhailov/dashsync/internal/model"

// Renderer turns a set of Groups into one dashboard's config file. It's
// deliberately small: Name identifies the renderer for --format flags and
// log lines, DefaultPath is where the CLI writes the result absent an
// explicit --output-path, and Render does the actual work.
//
// Groups is expected to already be in its final, deterministic order — the
// output of model.GroupServices, which sorts groups by name and services
// within a group by (Name, ID). Render's own job is narrower: reproduce
// that order faithfully in the target format, not decide it. Concretely,
// that means never ranging over a Go map to build the output list — doing
// so would reintroduce the exact nondeterminism GroupServices exists to
// remove, just one layer downstream of it.
type Renderer interface {
	Name() string
	DefaultPath() string
	Render(groups []model.Group) ([]byte, error)
}
