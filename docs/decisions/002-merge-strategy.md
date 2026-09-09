# 002 — Idempotent merge strategy

## Context

M2 (`internal/render`) produces a complete `services.yaml` from scratch on
every run. That's fine for `sync` printed to stdout, but writing it
directly over an existing file would destroy two things dashsync exists to
respect: comments, and any service a human added by hand. Turning the
existing file into Go structs and back (`Unmarshal` → merge in Go → `Marshal`)
was rejected from the very first design note in this project — it can't
round-trip either of those, full stop, regardless of how careful the
merge logic on top of it is.

The alternative is walking the existing file as a YAML AST
(`github.com/goccy/go-yaml/ast`) and only touching the nodes dashsync
itself is responsible for.

## Decision

**Ownership is marked per entry, not per file or per group.** Every
service entry dashsync writes gets a head comment:

```yaml
- Media:
    # dashsync:managed id=e95da3facf4e content=c5b9cefa
    - Jellyfin:
        href: http://10.0.0.5:8096
```

`id` is `model.Service.ID` (stable across runs for the same Docker
container — see ADR 001's discovery design). `content` is a fingerprint of
what dashsync wrote for that entry last time.

**The merge is a real three-way compare**, not two:

1. **Desired** — this run's freshly discovered `model.Service`.
2. **Managed** — what dashsync itself last wrote, recovered from the
   marker's `content` hash — not from a separately stored history file,
   the marker *is* the record.
3. **Actual** — the entry's current bytes in the file, which may or may
   not match Managed if a human edited it since.

Comparing `Actual` against `Managed` (not against `Desired`) is what tells
a legitimate upstream change (Docker label edited, container's port
changed) apart from a human's hand edit: if `Actual` still matches
`Managed`, nothing has touched this entry since dashsync wrote it, so it's
safe to overwrite with `Desired` — an ordinary sync, not a conflict. If
`Actual` diverges from `Managed`, a human changed something dashsync isn't
aware of, and `ConflictPolicy` decides what happens next:

- **Preserve** (default) — leave the entry exactly as it is, permanently,
  and report it. Overwriting a deliberate human edit is a worse failure
  mode for an infrastructure tool than dashsync falling one field behind
  on that one entry.
- **Overwrite** — replace it with `Desired` anyway.
- **Fail** — abort the entire merge before writing anything, so a human
  looks at it and decides.

**An entry with no marker, or a marker-shaped comment written by a human,
is never touched, ever**, regardless of content. `parseMarker` only
recognizes dashsync's own literal prefix; anything else — including a
comment that happens to start with `#` right above a list item — is
unmanaged by definition. There's no "adopt this entry" mechanism, and it's
deliberately absent: dashsync tracks what *it* wrote, not what a file
happens to contain.

**Removal doesn't get the same hand-edit protection update does.** Once a
service's ID is gone from `Desired` — the container disappeared, or its
`dashsync.group` label moved it elsewhere — its managed entry is removed
unconditionally, hand-edited or not. Preserve's whole point is "don't
overwrite content dashsync doesn't understand anymore"; keeping a stale
entry around for a container that no longer exists doesn't serve that. A
human who wants to keep something deletes dashsync's marker comment,
which turns it into an entry dashsync no longer considers its own.

## Alternatives considered

- **Unmarshal into Go structs, mutate, re-marshal.** Rejected outright —
  loses comments and key order, the two things this feature exists to
  keep. Not a real contender; noted here only because it's the obvious
  first instinct.
- **A separate state file recording what dashsync last wrote**, instead of
  embedding the fingerprint in the marker comment. Rejected: a second file
  can drift out of sync with the actual `services.yaml` (edited
  independently, or one committed to git without the other), and the
  entry it describes and the record of what it should look like would no
  longer be the same file a human is looking at when they wonder "did I
  edit this?" Embedding the hash in the comment keeps the record and the
  content it describes physically inseparable.
- **A full content diff instead of a hash** for hand-edit detection.
  Rejected for now as unnecessary complexity: a hash answers "did anything
  change" exactly as well for the one decision that depends on it
  (hand-edited or not), and a wrong "yes" costs nothing worse than an
  entry landing in Preserve when Overwrite would've been harmless too.
- **Cryptographic hash (sha256) for the content fingerprint.** Rejected —
  the fingerprint defends against an accidental mismatch (a human's edit),
  not a deliberate forgery, so FNV-32a is enough and is far cheaper.

## Consequences

- A `services.yaml` can genuinely be hand-edited between syncs without
  fear, as long as dashsync's own marker comments are left alone — which
  is the entire feature this project sells itself on.
- Removing dashsync's marker comment from an entry, by hand, is the
  supported way to say "stop managing this" — worth documenting for users,
  not just here.
- Two services can't be told apart if they'd hash to the exact same
  rendered content *and* the exact same ID, but that would require two
  identical `(Host, Container)` pairs, which `Discover()` can't produce
  from a single `ContainerList` call — Docker enforces container-name
  uniqueness within one daemon.

## Known limitations (found during review, deliberately not fixed here)

- **A human comment sharing a line block with a marker doesn't survive an
  update.** `buildMarker` always writes exactly one comment line; if a
  human appends their own note directly under a marker (no blank line
  between them, so it reads as the same comment group), that note is
  replaced along with the marker the next time the entry's tracked fields
  legitimately change. The fix — splicing a human's lines back in instead
  of replacing the whole comment group — needs `internal/merge` to parse
  and reconstruct multi-line comment groups, which nothing currently
  requires it to do. Recommended workaround until then: put a note like
  that on its own line, one blank line away from the marker, so it isn't
  part of the same comment group at the YAML level in the first place.
- **A hand-typed group or service name that YAML would decode as a
  non-string scalar** — a bare `123`, or a bare `yes`/`no`/`true`/`null` —
  fails `nodeKeyString`'s type assertion and is treated as "no existing
  match," producing a duplicate entry instead of merging into the
  original. dashsync's own output always quotes such names (`"123":`), so
  this only bites a *pre-existing*, hand-authored file with an unquoted
  ambiguous name — a narrow but real gap.
- **A marker comment embedded inside a flow-style items list isn't
  recognized as a marker.** `readEntries`'s comment extraction (`seqEntry`,
  `ast.go`) reads `seq.GetComment()`/`seq.ValueHeadComments` — fields
  goccy/go-yaml only ever populates for a block-style sequence's entries.
  A human (or a formatter) can legally collapse an items list dashsync
  already manages into flow style while leaving the marker comment
  physically present inside the brackets — `items: [{# dashsync:managed
  id=... content=...\n name: Jellyfin, url: http://x}]` parses without
  error, but empirically (verified directly against goccy/go-yaml v1.19.2,
  not just reasoned about — see the correction below) the comment token
  isn't attached to *any* AST node at all in that position: not the
  sequence, not the nested mapping's first key. It's simply absent from
  `f.String()`'s output the moment anything reserializes the document,
  which is a step *before* dashsync-specific logic ever runs. Either way
  `readEntries` finds no marker, the next update appends a brand-new entry
  with a fresh one, and the original — now permanently unmarked — becomes
  inert duplicate content dashsync no longer manages or ever removes.
  (An earlier version of this note claimed the comment "attaches to the
  nested mapping's own first key instead" — checked directly against the
  library rather than assumed, and that specific claim was wrong: it
  doesn't attach anywhere. The corrected mechanism doesn't change the
  consequence or the fix's absence, only the "why.")

  This is narrower than it first looks, and better understood in light of
  `writeEntries`'s own doc comment (`ast.go`): a group's items sequence
  gets force-flipped to block style by `writeEntries` on *every* sync that
  touches that group at all (add, update, *or* remove) — so the exposure
  window is only a hand-authored file's *first* sync after being put into
  this exact shape, for the one entry whose marker sits at the exact
  embedded position that gets lost. Every ordinary case checked
  empirically alongside this one round-trips correctly: a trailing
  comment after a flow sequence or mapping, a comment on its own line
  before a flow value (repositioned, not lost), and — the realistic
  version of "a managed items list has flow-style entries" — a
  *block-style* items list whose individual entries are flow-style
  mappings, which is exactly what
  `TestNamedGroupAdapter_ExistingFlowStyleItemsListGetsBlockStyle` already
  exercises and which round-trips fine. Reproducing the actual loss needs
  the *outer* items list to also be flow-style, with the comment placed
  right after one entry's opening `{` — specifically what a human
  guessing at how to hand-collapse a managed entry into flow style, marker
  included, would have to construct on purpose. Confirmed pre-existing
  (reproduces against `homepageAdapter` unmodified, not something
  `NamedGroupAdapter` introduced), and distinct from — not fixed by — ADR
  007's `normalizeGroupsListStyle`/`normalizeMappingFlowStyle`, which only
  normalize a container about to receive new content, not a comment
  already inside one that's about to be read. No workaround short of not
  hand-collapsing a managed items list to flow style in the first place.
- **No cross-process locking.** Two `sync --output-path` invocations
  against the same file, overlapping in time (two cron schedules, a CI
  matrix sharing a path), can each read the same starting content,
  compute independent merges, and the second one to finish wins — the
  first's changes are silently lost, and neither run reports a conflict,
  because they never see each other. Serializing calls against the same
  `--output-path` is the caller's responsibility for now.
