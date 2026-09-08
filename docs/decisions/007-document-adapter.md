# 007 — Generalizing `internal/merge` via `DocumentAdapter`

## Context

ADR 003 deferred generalizing `internal/merge` past Homepage's specific
document shape, explicitly on the grounds that one second format (Homer)
wasn't enough evidence to design against — but it named its own trigger for
revisiting: "the next renderer (Dashy, or a real second attempt at Homer
merge support) is the right moment... now with two concrete shapes already
in hand to check it against instead of one imagined shape and one real
one."

Dashy is that next renderer. Its schema
(`sections: [{name, icon, items: [{title, url, ...}]}]`, nested inside an
otherwise-foreign document — `pageInfo`, `appConfig`, `pages`) turned out to
be the same shape family as Homer's `services: [{name, items: [{name,
url, ...}]}]`, differing only in the item-identity field (`title` vs
`name`). That's ADR 003's own condition met, not a reopening of it against
its own terms.

## Decision

Introduce `merge.DocumentAdapter` (`internal/merge/adapter.go`): the
document-shape-specific half of merge support — locating (and, on a first
write, creating) a document's list of groups, and a group's own list of
entries. `EntryRenderer` grows one method, `Adapter() DocumentAdapter`,
since the pairing between a renderer and how its document is shaped is
intrinsic to the format, not a caller choice.

This lands as three separate PRs, specifically to avoid ADR 003's warned-of
failure mode ("abstraction built for one imagined case, wrong the moment a
second real case shows up") by proving each layer before building the next
on top of it:

1. **This PR** — introduce `DocumentAdapter` with `homepageAdapter` as the
   only implementation: today's `rootSequence`/`findOrCreateGroupSequence`
   logic and the old inline removal-pass loop, moved behind the interface
   verbatim. Pure refactor — `merge_test.go` passes with zero changes to the
   file itself. This step alone proves the interface shape doesn't fight
   the one case already known to work, before any new shape touches it.
2. Add the Dashy renderer as render-only (matching Homer's current state) —
   decoupled from any `internal/merge` work, so "does the renderer map
   fields correctly" and "does the new AST code work" get reviewed
   separately.
3. Add `NamedGroupAdapter` — the actual new AST mechanics (finding/creating
   a key nested in an existing, otherwise-foreign mapping, which behaves
   differently from `internal/merge`'s existing sequence-splicing code) —
   wired into **both** Homer and Dashy at once, which is what proves the
   abstraction serves two real consumers rather than one real case plus a
   sketch. `internal/merge`'s hand-edit-detection and identity machinery
   (`readEntries`/`writeEntries`/`applyDesired`/`removeOrphaned`/
   `marker.go`) needs no changes for this step: it operates purely on
   `*ast.SequenceNode` entries and is already format-agnostic, which is
   what makes isolating the new shape's specifics to `DocumentAdapter`
   possible in the first place — see individual PRs for anything that
   turns out not to hold once `NamedGroupAdapter` is actually built.

## What this step (1 of 3) deliberately does not yet claim

`DocumentAdapter` is verified sound for exactly one shape (Homepage's) — it
has not yet been checked against a foreign-nested shape, because
`NamedGroupAdapter` doesn't exist yet. Two things flagged during this PR's
own review, worth recording rather than glossing over:

- `parseOrEmpty` (`internal/merge/ast.go`) still hardcodes "no file yet" as
  a bare `[]byte("[]\n")` — Homepage's own shape, which is why
  `homepageAdapter.GroupSequence` needs no bootstrap logic of its own
  (`rootSequence` just returns that placeholder as-is). A foreign-nested
  shape's document root is never a bare sequence, so `NamedGroupAdapter`'s
  own `GroupSequence` will need to detect that specific placeholder and
  replace it with a minimal fresh `{topLevelKey: []}` document — a new
  responsibility for step 3, not one already solved generically by this
  interface.
- `DocumentAdapter`'s methods traffic directly in `goccy/go-yaml/ast` node
  types. This matches `EntryRenderer.NormalizeEntry(node ast.Node)`'s
  existing precedent (every renderer implementing merge support already
  imports `ast`), so it isn't new coupling — but it does mean a renderer
  package can't implement merge support without depending on this
  project's specific YAML AST library. Accepted as consistent with the
  existing design, not reconsidered here.

## Alternatives considered

Both alternatives ADR 003 listed for its own milestone are superseded here,
not reversed on new reasoning:

- *"Generalize `internal/merge` now, via a `DocumentAdapter`-shaped
  capability"* — ADR 003 rejected this specifically for lacking a second
  real data point beyond Homepage. That data point (Dashy, alongside Homer)
  now exists; this ADR is what ADR 003 called "revisit."
- *"Make every renderer implement `merge.EntryRenderer` or don't ship it"*
  — ADR 003 already reversed this position when Homer shipped render-only.
  Nothing here re-adopts it as a hard rule even though Homer and Dashy will
  both gain merge support by the end of step 3: the `mergeableRenderer`
  gate in `internal/cli/sync.go` stays, because nothing guarantees a
  *future* renderer's shape fits `DocumentAdapter` either.

## Consequences

- `EntryRenderer` is a wider interface than before (`Adapter()` added).
  `Merge()`'s own exported signature doesn't change.
- `internal/merge` now hosts format-specific adapter code
  (`homepageAdapter`, and `NamedGroupAdapter` from step 3) alongside its
  generic algorithm, rather than each renderer package owning its own
  adapter — a deliberate boundary, not an accident of where the extraction
  landed: adapters need direct access to unexported AST-manipulation
  helpers (`rootSequence`, `fixFlowStyleColumn`, and step 3's
  `getOrAppendField`) that are the actual hard-won part of this package,
  and exporting them just to let adapters live in `internal/render/*`
  would widen the public surface for no benefit.
- Homer's ADR-003 limitation (render-only) closes once step 3 lands, as a
  side effect of solving Dashy's merge support with the same code — not a
  goal pursued on its own.
