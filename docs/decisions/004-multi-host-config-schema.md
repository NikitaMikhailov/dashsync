# 004 — dashsync.yaml's host schema and validation rules

## Context

M4 adds support for discovering from more than one Docker daemon, driven
by an optional `dashsync.yaml` (`internal/config`). A missing file means
"just the local daemon" — the same zero-config behavior dashsync had
before this existed — so the schema and its validation only matter once
someone opts in by writing the file.

A hand-written YAML file is the entire interface here; nothing else
checks it before it reaches `internal/discovery`. That pushed most of the
design work into deciding what `validate()` should catch at load time
versus what's acceptable to let fail later, deeper in Docker client
construction.

## Decisions

**Address must be empty or a recognized scheme — and "recognized" means
"actually implemented by this project's dependency," not "a scheme Docker
somewhere accepts."** `address` accepts `tcp://` or `unix://`, or empty to
mean "use the environment's own Docker connection." ~~`ssh://` is a real,
documented Docker connection form, but `github.com/moby/moby/client` — the
SDK this project depends on — doesn't implement SSH transport:
`client.WithHost` hands an `ssh://` address straight to a plain TCP dialer,
which fails confusingly instead of tunneling over SSH the way writing
`ssh://` would reasonably lead someone to expect. Accepting it as valid
syntax with silently wrong behavior underneath would be worse than
rejecting it outright, so it's rejected the same way any other unsupported
scheme is.~~ `ssh://` is accepted too, as of
[ADR 009](009-ssh-docker-discovery.md) — the SDK gap described above is
still real and still why `client.WithHost` alone can't be trusted with it,
but dashsync now works around it directly instead of rejecting the scheme
outright; see that ADR for the mechanism. A scheme-less address like
`10.0.0.6:2376` — valid for
`$DOCKER_HOST`, invalid here — is rejected at load time too, instead of
silently producing a broken `Host.ResolveURLHost` result or an opaque
connection failure far from the config line that caused it. `npipe`
(Windows) is left out for an unrelated, simpler reason: dashsync doesn't
build for Windows, and there's nothing to protect by allowing a scheme
that can't be exercised — adding it back is a one-line change the day
that's no longer true.

**Duplicate detection compares addresses canonically, not byte-for-byte.**
Two hosts resolving to the same daemon — including two hosts that both
leave `address` empty — would enumerate its containers twice under two
different `Host.Name` values in the merged output. Catching only
byte-identical duplicates would miss `TCP://10.0.0.6:2376` and
`tcp://10.0.0.6:2376/` naming the same daemon, so the comparison lowercases
scheme and host and trims a trailing slash before comparing.

**Host names are compared case-insensitively.** `local` and `Local` would
be indistinguishable once they reach `inspect`'s HOST column or
`Service.Source.Host`, so treating them as distinct names would just move
the confusion from config-load time to output-reading time.

**TLS configuration must be coherent with the address it's attached to,
checked in a fixed priority order.** Rules, checked in this order so
which error wins when a host breaks more than one is a deliberate choice
(originally three; a fourth was added by [ADR 009](009-ssh-docker-discovery.md)
when `ssh://` support landed, in the same fixed-order list rather than as
an afterthought):

1. `tls` with no `address` is rejected — an empty address means "the
   environment's own Docker connection," which never consults this
   config's `tls` block, so the setting would be silently ignored.
2. `tls` on a `unix://` address is rejected — a unix socket has no TLS
   layer, so `client.WithTLSClientConfig` never comes into play, and the
   setting would just be silently ignored, same as above.
3. `tls` on an `ssh://` address is rejected — SSH already provides its
   own transport security, so a separate TLS layer is equally meaningless
   here, for the same reason as `unix://` above.
4. `tls.cert` and `tls.key` must both be set or both be empty — a CA-only
   block (server verification without a client certificate) is valid, but
   a cert without its key (or vice versa) fails deep inside
   `client.WithTLSClientConfig`'s underlying `tlsconfig.Client`, which
   only runs once a client is actually constructed for that host — by
   then the error can no longer name the config line that caused it.

**Unknown fields are a parse error, not a silently-ignored one.** `Load`
uses `yaml.DisallowUnknownField()`. A typo like `adress` instead of
`address` would otherwise decode as "no address at all," silently pointing
that host at the local daemon instead of the remote one the operator
wrote — indistinguishable from a deliberately-omitted field. Since
hand-edited YAML is the only interface, that ambiguity isn't acceptable
for something this consequential.

## Alternatives considered

- **Defer all of this to Docker client construction.** Rejected: every
  one of these failure modes was reproduced during review and, left
  unchecked, surfaces far from the config line that caused it — often
  attributed to whichever goroutine in a future `errgroup`-based
  multi-host fan-out happens to dial that host first, not to config
  loading. Catching them at `Load()` keeps the error message pointing at
  `hosts[i]`.
- **Case-sensitive, byte-exact comparisons for names and addresses.**
  Simpler, but both were shown to let real near-duplicates through
  silently (`local`/`Local`; `TCP://...`/`tcp://.../`) — the entire reason
  this validation exists is to fail loudly instead of double-counting or
  misrouting silently.

## Consequences

- A config author gets a specific, host-indexed error for most schema
  mistakes at `dashsync sync` startup, before any Docker connection is
  attempted.
- `internal/discovery`'s multi-host wiring (the next piece of M4) can
  assume every `config.Host` it receives has an empty-or-valid `Address`
  scheme and internally consistent `TLS`, and doesn't need to repeat these
  checks itself.
- The TLS rules are checked in a fixed order specifically so a host
  breaking more than one reports the same error every time — pinned down
  by `TestLoad_TLSViolationPriority_EmptyAddressWinsOverCertKeyMismatch`,
  not left to accidentally depend on `validate()`'s statement order.
