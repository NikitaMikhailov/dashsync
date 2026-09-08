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

This ADR is written from schema research (dashy.to's own documentation),
ahead of `internal/render/dashy` existing in code — this project's own
"tests before implementation" convention applied one level up, to the
design decision rather than a single test. Step 2 of the plan below is what
actually confirms it: `internal/render/dashy`'s implementation and test
suite (in particular `TestRenderer_Render_GroupIdentifiedByNameFieldItemByTitleField`)
verify the `name`/`title` divergence lands exactly where this ADR assumed
it would, before step 3 builds `NamedGroupAdapter` on top of that
assumption.

## Decision

Introduce `merge.DocumentAdapter` (`internal/merge/adapter.go`): the
document-shape-specific half of merge support — locating (and, on a first
write, creating) a document's list of groups, and a group's own list of
entries. `EntryRenderer` grows one method, `Adapter() DocumentAdapter`,
since the pairing between a renderer and how its document is shaped is
intrinsic to the format, not a caller choice.

This landed as three separate PRs, specifically to avoid ADR 003's
warned-of failure mode ("abstraction built for one imagined case, wrong the
moment a second real case shows up") by proving each layer before building
the next on top of it:

1. Introduce `DocumentAdapter` with `homepageAdapter` as the
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

## What step 1 alone deliberately didn't claim (resolved by step 3)

When only `homepageAdapter` existed, `DocumentAdapter` was verified sound
for exactly one shape. Two things flagged then, and how step 3 actually
resolved them:

- `parseOrEmpty` (`internal/merge/ast.go`) hardcodes "no file yet" as a bare
  `[]byte("[]\n")` — Homepage's own shape. The first version of
  `NamedGroupAdapter.GroupSequence` detected that placeholder purely by
  parsed shape (an empty `*ast.SequenceNode` root), but a harsh review pass
  caught the real problem with that: a genuinely *existing* file that
  happens to also be an empty sequence — a degenerate Homepage
  `services.yaml`, or simply the wrong `--output-path` for the format — is
  structurally identical to "doesn't exist," and would have been silently
  overwritten instead of reported as a mismatch. `DocumentAdapter.GroupSequence`
  gained an explicit `isNewDocument bool` parameter instead, computed by
  `Merge` from the raw input bytes before `parseOrEmpty` folds the two
  cases together — `homepageAdapter` ignores it (Homepage has no such
  ambiguity: an empty sequence root always legitimately means "zero
  groups," whether or not the file "really" existed), `NamedGroupAdapter`
  uses it as the sole bootstrap trigger. `parseOrEmpty` itself needed no
  change.
- `DocumentAdapter`'s methods traffic directly in `goccy/go-yaml/ast` node
  types. This matches `EntryRenderer.NormalizeEntry(node ast.Node)`'s
  existing precedent (every renderer implementing merge support already
  imports `ast`), so it isn't new coupling — but it does mean a renderer
  package can't implement merge support without depending on this
  project's specific YAML AST library. Accepted as consistent with the
  existing design, not reconsidered here.

Step 3 added one more genuinely new mechanic beyond what step 1 anticipated:
appending a field (`items:`, or the top-level key itself) into an *existing*
`*ast.MappingNode` — a hand-written group missing `items:`, or a document
with foreign settings but no `services:`/`sections:` yet. This behaves
differently from splicing a new entry into a `*ast.SequenceNode` (which
`fixFlowStyleColumn` already handled): a `*ast.MappingNode`'s own rendering
doesn't strip a spliced field's absolute column the way a sequence entry's
leading `"- "` does, so the new field needs an explicit `ast.Node.AddColumn`
correction first — the same primitive `goccy/go-yaml`'s own
`ast.MappingNode.Merge` uses for an identical purpose. Read from
`goccy/go-yaml@v1.19.2`'s own source before writing any of
`internal/merge/adapter.go`'s `getOrAppendField`, then confirmed with a
dedicated empirical spike (`internal/merge/getorappendfield_test.go`,
written and run *before* the rest of `NamedGroupAdapter`) covering the
specific case that mattered most: appending a fresh top-level key next to
real, hand-written foreign settings, plus the one-level-deeper case
(appending `items:` inside an existing group entry). Both matched the
source-reading exactly on the first run — see that test file's own comment
for why `IndentLevel` (a second field goccy's rendering also consults,
separately from `Column`) didn't turn out to need the same correction.

## A critical bug the empirical spike alone didn't catch

The `getOrAppendField` spike (above) proved the *column* correction for a
field being found or appended. It didn't cover a distinct failure mode a
harsh review pass found by actually trying a hand-written flow-style
top-level groups list against the real `homer.New()` renderer:
`services: [{name: Media, items: []}]`. Merging a new service into that
document produced YAML that failed to re-parse on the very next run —
breaking idempotency outright, the property this whole package exists to
guarantee.

The root cause was two-fold, both variants of "a flow-style container
can't legally hold the block-style content Merge is about to splice into
it," found this same way ADR 002's three bugs were — by actually running
the code against something real, not just reasoning from the source:

1. **The groups list itself** (`services: [...]`) staying flow-style even
   though one of its groups was about to gain block-style content (a
   marker comment, a multi-line entry). `fixFlowStyleColumn` only ever
   corrected a sequence's *column*, never its `IsFlowStyle` flag — nothing
   forced it false except the "append a brand-new group" path, which
   doesn't run when every desired group already exists in the file.
   `DocumentAdapter.GroupSequence` now normalizes this (`ast.go`'s
   `normalizeGroupsListStyle`) once it holds at least one real group,
   regardless of which group actually changes.
2. **A group entry itself** (`- {name: Media, items: []}`) hand-written as
   a flow-style mapping with its `items` field already present. Flipping
   `IsFlowStyle` alone still produced garbled indentation, because a flow-
   style mapping's fields carry mutually inconsistent columns from
   wherever they sat on one line — `ast.SequenceNode`'s block-style
   rendering measures how many leading characters the *first* line of an
   entry needs stripped, then strips exactly that many from every
   subsequent line before re-indenting uniformly. Two fields that don't
   share a column (the flow-style case) violate that assumption; two that
   do (the normal block-style case, or a freshly-`yaml.Marshal`ed entry,
   which is why "create a new group" already worked) satisfy it by
   construction. `getOrAppendField` now runs a new `normalizeMappingFlowStyle`
   first, unconditionally, on the mapping it's about to find-or-append a
   field in: flips `IsFlowStyle` and recolumns *every* existing field to a
   shared baseline, not just the one field this particular call cares
   about.

Both fixes are covered by dedicated tests at the adapter level
(`TestNamedGroupAdapter_FindOrCreateGroup_ConvertsFlowStyleGroupsListToBlockStyle`,
`_ConvertsFlowStyleGroupEntryToBlockStyle`) and by re-running the exact
scenario end to end (`TestNamedGroupAdapter_ExistingFlowStyleItemsListGetsBlockStyle`
plus a manual run against a real Docker daemon confirming a second
`sync --output-path` reports no changes against the file the first run
produced). The lesson generalizes past this specific bug: an AST-level fix
verified only by tracing what one specific field's rendering does, without
trying the actual scenario a hand-written file could produce, is exactly
the gap a harsh review — not just careful source-reading — exists to close.

## Alternatives considered

Both alternatives ADR 003 listed for its own milestone are superseded here,
not reversed on new reasoning:

- *"Generalize `internal/merge` now, via a `DocumentAdapter`-shaped
  capability"* — ADR 003 rejected this specifically for lacking a second
  real data point beyond Homepage. That data point (Dashy, alongside Homer)
  now exists; this ADR is what ADR 003 called "revisit."
- *"Make every renderer implement `merge.EntryRenderer` or don't ship it"*
  — ADR 003 already reversed this position when Homer shipped render-only.
  Nothing here re-adopts it as a hard rule even though Homer and Dashy both
  gained merge support by the end of step 3: the `mergeableRenderer` gate
  in `internal/cli/sync.go` stays, because nothing guarantees a *future*
  renderer's shape fits `DocumentAdapter` either.

## Consequences

- `EntryRenderer` is a wider interface than before (`Adapter()` added).
  `Merge()`'s own exported signature doesn't change.
- `internal/merge` now hosts format-specific adapter code
  (`homepageAdapter` and `NamedGroupAdapter`) alongside its generic
  algorithm, rather than each renderer package owning its own adapter — a
  deliberate boundary, not an accident of where the extraction landed:
  adapters need direct access to unexported AST-manipulation helpers
  (`rootSequence`, `fixFlowStyleColumn`, `getOrAppendField`) that are the
  actual hard-won part of this package, and exporting them just to let
  adapters live in `internal/render/*` would widen the public surface for
  no benefit.
- Homer's ADR-003 limitation (render-only) closed as a side effect of
  solving Dashy's merge support with the same `NamedGroupAdapter` — not a
  goal pursued on its own. `sync --format homer --output-path` and
  `sync --format dashy --output-path` both work today, verified against a
  real Docker daemon (two labeled containers, `--output-path` twice each,
  confirming idempotency in practice) in addition to
  `internal/merge/namedgroup_test.go`'s unit coverage.
