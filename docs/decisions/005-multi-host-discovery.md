# 005 — Multi-host discovery: concurrency and failure isolation

## Context

With `internal/config` (ADR 004) in place, `internal/discovery` and
`internal/cli` needed to actually use it: discover from every configured
host, not just one, without one unreachable daemon stopping the others or
one slow daemon serializing behind the rest.

## Decisions

**Concurrency via `sync.WaitGroup`, not `golang.org/x/sync/errgroup`.**
`errgroup` is the obvious reach for "run N things concurrently, collect
results" — but its main value over a plain `WaitGroup` is coordinated
early cancellation when any goroutine returns an error, and this exact
feature is what the failure-isolation requirement below rules out: a
`DiscoverAll` goroutine must *never* propagate its own host's failure into
shared cancellation, so the only thing `errgroup` would add here is a new
dependency (still outside `go.mod`'s allowlist, a `depguard` change and a
discussion of its own per `CLAUDE.md`) for a feature this code deliberately
doesn't use. `discoverAll` pre-sizes a `[]HostResult` once, hands each
goroutine its own index (safe under Go 1.22+'s per-iteration loop variable
semantics, which `go.mod`'s `go 1.26` guarantees), and waits — no shared
mutable state beyond that, verified race-free by `-race`.

**A host's failure is a warning, never an error — until every host
fails.** `DiscoverAll` returns `[]HostResult`, each independently
succeeded-or-failed; `internal/cli/root.go`'s `aggregateHostResults` turns
a failed host into an `error` appended to a `[]error` "warnings" slice, not
an early return, and only produces a fatal `error` when *every* configured
host failed (nothing left to report). `newInspectCmd`/`newSyncCmd` print
warnings to stderr via `printWarnings` and continue with whatever succeeded
— matching the project's stated multi-host requirement: one host down
means a warning, not a broken run.

**Connecting to a host and listing its containers are the same failure
mode.** `HostResult.Err` doesn't distinguish "couldn't connect" from
"connected but `ContainerList` failed" — both mean "nothing usable from
this host this run," and a caller deciding whether to warn or abort didn't
need the distinction. `discoverOneHost` folds both into one `Err` field.

**`dockerConnector` takes `ctx context.Context` as its first parameter,
even though today's only implementation doesn't use it.** `client.New`
doesn't dial — building a `*client.Client` is local work (an HTTP
transport, optionally reading TLS files) — so there's nothing to cancel
today. But `dockerConnector` is the seam a future connection method (an
SSH tunnel dial, say — see ADR 004's note on why `ssh://` isn't supported
yet) would need to bound by the caller's deadline, and this project's own
convention is `context.Context` first, always. Threading it through now
costs nothing and avoids a second signature break later.

**One shared `dockerCallTimeout` budget across all hosts, not one per
host.** `discoverDocker` wraps a single 10s `context.WithTimeout` around
the whole `DiscoverAll` call. Because hosts run concurrently, this reads as
"the slowest of N hosts must answer within 10s," not "each host
individually gets 10s" — a deliberate simplification for now. A per-host
override would need its own `config.Host` field (alongside `TLS` and
`URLHost`, which already exist) if host count or per-host latency ever
makes a shared budget too tight in practice; nothing about today's design
forecloses adding one.

## Alternatives considered

- **`errgroup.WithContext`, deliberately not returning errors from
  goroutines (always `return nil` to the group).** This would work, but
  buys nothing over `WaitGroup` while still requiring the new dependency —
  `errgroup`'s entire value proposition (context cancellation on first
  error) is exactly what this design avoids using.
- **Distinguish connect failures from list failures in `HostResult`.**
  Rejected: no caller needed to branch on which one happened, and Go's
  error-wrapping (`fmt.Errorf("... %w", err)`) already preserves the
  distinction in the message text for anyone reading warnings, without a
  second field nothing consumes.
- **Fail the whole command on the first host failure**, matching how a
  single-host `dashsync` behaved before this milestone. Rejected outright
  per the project's own multi-host requirement — the entire point of
  running against several hosts is that one being down doesn't take the
  others with it.

## Consequences

- Adding a real second connection method (SSH, or anything else
  `client.New` doesn't support out of the box) means implementing a new
  `dockerConnector`, not redesigning the fan-out — the seam already exists
  and already carries `ctx`.
- A silent typo or a genuinely broken host doesn't fail `sync` outright;
  it prints a warning and continues. That's the intended behavior, but it
  does mean a broken host can go unnoticed if nobody reads stderr (a
  cron-triggered `sync` piping only stdout somewhere, for instance) — worth
  keeping in mind if this project ever adds a machine-readable output mode.
- `internal/discovery`'s test suite exercises the real `connectDocker` /
  `NewDockerClientForHost` chain end-to-end (via a loopback address with a
  guaranteed, immediate connection-refused) alongside the fake-connector
  unit tests, rather than leaving the real wiring path checked only by the
  real-Docker integration tier.
