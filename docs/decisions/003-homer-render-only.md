# 003 — Homer gets Render only, not the idempotent merge, for now

> Superseded by [007](007-document-adapter.md) once Dashy's shape confirmed
> Homer's generalizes — this record is kept as-is for the reasoning that
> led there.

## Context

M4 adds a second dashboard format specifically to answer the checkpoint
ADR 002 left open: does `render.Renderer` hold up against a second real
format, or does it leak? Homer (`github.com/bastienwirtz/homer`) was
picked for the reason the original project plan gave — it has no built-in
Docker discovery of its own, so it's where dashsync adds the most value —
and turned out to differ from Homepage in exactly the ways that matter for
`internal/merge`, not for `render.Renderer` itself.

Homepage's `services.yaml` *is* the managed document: every top-level
entry is a group, every group is a single-key map, every service inside it
is a single-key map. `internal/merge`'s whole AST-walking design —
`rootSequence` treating the document's root as the groups list,
`findOrCreateGroupSequence` matching a group by an entry's one map key —
is built on that shape.

Homer's `config.yml` is mostly *not* dashsync's to manage. `title`,
`theme`, `colors`, `columns`, and a dozen other keys are configured by a
human directly; dashsync only ever owns the `services:` key's contents.
And within that, a group is `{name: ..., icon: ..., items: [...]}` — a
regular multi-field object identified by a `name` *field*, not by being
its own map key, and Homer's `items` are the same shape one level down.

## Decision

`render.Renderer` needed no changes — `homer.Renderer` implements `Name`,
`DefaultPath`, and `Render` exactly like `homepage.Renderer` does, and
`sync --format homer` (without `--output-path`) works today. The interface
that *doesn't* generalize is `merge.EntryRenderer`, and by extension the
`--output-path` idempotent-merge path: `internal/merge` doesn't know how
to find a "services:" key buried in an otherwise-foreign document, or
match a group by a `name` field instead of a map key.

Rather than force that generalization now, on a single second data point,
`homer.Renderer` simply doesn't implement `merge.EntryRenderer`.
`internal/cli/sync.go`'s renderer registry became `map[string]render.Renderer`
(the common denominator both formats share), with a type assertion to a
`mergeableRenderer` interface gating the `--output-path` path specifically.
Passing `--format homer --output-path <file>` returns a clear error naming
both the format and the flag, rather than silently falling back to
stdout-only behavior or (worse) attempting a merge that would corrupt a
real `config.yml`'s unrelated settings.

## Alternatives considered

- **Generalize `internal/merge` now**, via a `DocumentAdapter`-shaped
  capability (locate/create the groups sequence within an arbitrary
  document, locate/create a group by whatever identity a format uses,
  same one level down for items) that both Homepage and Homer would
  implement. Rejected for *this* milestone: `internal/merge`'s AST
  handling was already the single hardest, most bug-prone part of this
  codebase to get right for one format (see ADR 002's three empirically-
  found bugs) — generalizing it on the *first* second-format encounter,
  before a third data point exists to check the abstraction against, risks
  the exact "abstraction built for one imagined case, wrong the moment a
  second real case shows up" failure this project's own conventions warn
  against elsewhere. Revisit once a third format's shape is known, or once
  someone actually asks for idempotent Homer sync.
- **Make every renderer implement `merge.EntryRenderer` or don't ship it**
  (the position CLAUDE.md and ADR 002 both took before this milestone).
  Reversed here: that stance was written having only ever built one
  renderer, and turned out to be a claim about the interface's generality
  that hadn't actually been tested against a second format yet.

## Consequences

- `sync --format homer` is render-only until `internal/merge` grows a
  document-shape abstraction. Documented in `homer.Renderer`'s own package
  comment and enforced by a type assertion, not just a comment — the
  failure mode for an unsupported combination is a clear error message,
  not a silent no-op or a corrupted file.
- The next renderer (Dashy, or a real second attempt at Homer merge
  support) is the right moment to design `internal/merge`'s document
  abstraction, now with two concrete shapes already in hand to check it
  against instead of one imagined shape and one real one.
