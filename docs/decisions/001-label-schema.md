# 001 — Docker label schema

## Context

dashsync discovers services by reading Docker container labels. Two
existing schemes are worth knowing before designing a third: Homepage uses
flat dotted keys (`homepage.name`, `homepage.group`, `homepage.icon`, ...)
scoped to its own config surface; AutoKuma uses `kuma.<id>.<type>.<setting>`,
where `<id>` lets one container register several monitors and `<type>`
picks which Uptime Kuma monitor type's settings follow.

dashsync only ever maps *one* container to *one* service (no equivalent of
AutoKuma's multi-monitor-per-container need), but it does need to carry
renderer-specific fields (M2+: a Homepage widget config, a Homer subtitle,
...) without the core schema knowing about every renderer in advance.

## Decision

- `dashsync.enable` — must be exactly `"true"` to opt in. Everything else is
  ignored for a container that doesn't set this. **Opt-in, not opt-out**:
  an unlabeled container never shows up on a dashboard by accident just
  because it happens to publish a port. This is the one place the schema
  intentionally departs from Homepage (which is opt-out once its label
  discovery is turned on globally) — dashsync runs against every container
  on every host by design (multi-host, M4), so silence-by-default matters
  more here than it does for a single-node dashboard.
- `dashsync.name`, `dashsync.group`, `dashsync.url`, `dashsync.icon`,
  `dashsync.description` — flat, like Homepage's scheme. There's no
  identifier segment (`<id>`) because there's nothing to disambiguate: one
  container is one service, full stop.
- `dashsync.<anything else>` — collected verbatim into `Service.Extra`,
  keyed by whatever follows `dashsync.`. A renderer picks its own fields out
  of this map by whatever prefix convention it wants (e.g. a Homepage
  renderer would look for `homepage.*` keys). This is the
  AutoKuma-flavored part of the decision: `<type>` there is exactly this
  problem — namespacing renderer-specific data — solved with an explicit
  extension point instead of a fixed enum of renderer names in the core
  schema.

## Alternatives considered

- **`dashsync.<renderer>.<id>.<type>.<setting>`**, i.e. AutoKuma's full
  scheme adapted — rejected: there is no `<id>` disambiguation need (see
  above), and forcing every label under a `<renderer>` segment even for the
  five fields every renderer needs (name, group, url, icon, description)
  would mean repeating them per renderer instead of setting them once.
- **Opt-out** (every container with a label at all is shown, matching
  Homepage) — rejected for the reason above: multi-host discovery means
  dashsync sees containers nobody meant to expose.

## Consequences

- A renderer-specific field lives in `Extra` with no compile-time schema —
  a typo in `dashsync.homepage.widget.type` silently produces an unused
  `Extra` key rather than a validation error. Acceptable for now; worth
  revisiting once there's more than one renderer's worth of real-world
  typos to learn from.
- `dashsync.enable=true` on every container that should be visible is one
  more label to remember per service, compared to an opt-out scheme. The
  read-only, multi-host nature of the tool makes that a fair trade.
