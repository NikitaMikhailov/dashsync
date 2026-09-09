# 009 — SSH-tunneled Docker discovery

## Context

`internal/config/config.go`'s `validAddressSchemes` used to reject
`ssh://` outright: `github.com/moby/moby/client` — the only Docker SDK
this project depends on — doesn't implement SSH transport on its own.
`client.WithHost("ssh://...")` silently fell through to a plain TCP
dialer (`sockets.ConfigureTransport`'s default case), which would fail
confusingly rather than tunnel the way writing `ssh://` reasonably
suggests it should. Accepting the syntax with silently wrong behavior
underneath would have been worse than rejecting it, so it was rejected
the same way any other unsupported scheme is — with a comment naming
`docker/cli`'s own `connhelper/ssh` package as "the reference
implementation" for a real fix, and noting that pulling a new dependency
in for it would be "a dependency discussion of its own."

## Decision

`ssh://user@host[:port]` is now a valid `Host.Address` — implemented by
reproducing the mechanism Docker's own official `ssh://` support actually
uses, in this project's own code, rather than vendoring a new SSH client
library.

**Mechanism: exec the user's own `ssh`, running `docker system
dial-stdio` on the far end.** `docker/cli`'s `connhelper/ssh` +
`commandconn` packages work by shelling out to the local `ssh` binary to
run one fixed remote command and treating that subprocess's stdin/stdout
as the raw HTTP connection to the remote daemon. `internal/discovery/
sshconn.go` does the identical thing directly: `commandConn` wraps an
`*exec.Cmd`'s pipes as a `net.Conn`, `sshDockerDialer` builds the command,
and `newSSHDockerClient` wires that dialer into `client.New` via
`client.WithDialContext`.

**Zero new dependencies.** Only `os/exec`, `net`, `net/url`, `context` —
all `$gostd`-allowed already. The alternative, vendoring
`golang.org/x/crypto/ssh`, was considered and rejected: it would mean
owning host-key TOFU verification, agent-protocol forwarding, and
`~/.ssh/config` semantics (`ProxyJump`, `IdentityFile`, `Include`)
ourselves — real security-sensitive surface area, for strictly worse
behavior than the user's already-configured, already-trusted system
`ssh` gives for free the moment it's simply exec'd.

**No new `Host` config fields.** Identity file, agent settings, host
aliases — all of it is the user's own `~/.ssh/config`, exactly like
Docker's own `ssh://` support. There's exactly one reasonable place for
each of those to already live, and it isn't a second dashsync-specific
config surface.

**Two implementation details worth recording, both found by reading the
actual vendored source rather than assumed:**

- `client.WithHost(...)` must be listed *before*
  `client.WithDialContext(...)` in `client.New(...)`. `WithHost` calls
  `sockets.ConfigureTransport`, which overwrites `transport.DialContext`
  with a plain dialer as its default-case behavior — passed in the other
  order, the custom SSH dialer would be silently clobbered.
  `client.WithHost("http://" + client.DummyHost)` — the SDK's own
  placeholder for "never actually resolved" — is used as the host value,
  since the custom dialer ignores whatever network/addr the HTTP layer
  passes it and always reaches the fixed SSH target instead.
- `commandConn.Close()` kills the process, then waits for it — not a
  manual `stdin`/`stdout` close. `exec.Cmd`'s own documented contract is
  that `Wait` is what actually closes the pipes once the process exits,
  and calling it before every read has finished is documented as
  incorrect. A `sync.Once`-guarded `wait()` is shared between `Close()`
  and `annotate()` (below), since `exec.Cmd` documents a second `Wait()`
  call as an error and both a natural EOF and an explicit `Close()` can
  plausibly race to trigger it.
- Captured stderr is folded into any read/write error
  (`commandConn.annotate`), and `annotate` calls `wait()` first,
  unconditionally — `cmd.Stderr` is drained by a goroutine `exec.Cmd`
  itself starts on `Start()`, and that goroutine finishing is only
  guaranteed once `Wait()` returns. Reading stdout's EOF races that
  goroutine's last write into the stderr buffer; skipping the wait was
  tried first during development and produced exactly that race (`ssh`'s
  own diagnostic text missing from the surfaced error about half the
  time) before this fix.

## Consequences

- **The remote host needs the `docker` CLI reachable for the SSH'd-in
  user, not just a running daemon.** `docker system dial-stdio` is a
  `docker` CLI subcommand — the same constraint Docker's own official
  `ssh://` support has, so it's well-precedented, not a dashsync-specific
  weakness. A host running only `dockerd` with no client installed can't
  be reached this way.
- **`BatchMode=yes` means no interactive host-key prompt is possible.**
  The first connection to a host `ssh` doesn't already recognize must be
  accepted via a manual, interactive `ssh` run once before dashsync can
  reach it — a non-interactive discovery run has no way to answer a
  "are you sure you want to continue connecting" prompt, so it fails
  fast and clearly instead of hanging. Worth a line in the README, not
  just here — it's the first thing a new user pointing dashsync at a
  fresh host will hit.
- **`addressScheme`'s canonical-form dedup strips SSH userinfo.**
  `ssh://alice@10.0.0.6` and `ssh://bob@10.0.0.6` canonicalize identically
  (`url.URL.Host` never includes userinfo) and are flagged as duplicate
  addresses. Deliberate: it's the same daemon regardless of which user
  connects.
- `config.validate()`'s TLS-coherence check now has a third rejected
  scheme alongside `unix` — `ssh` already provides its own transport
  security, so a `tls:` block on an `ssh://` host is rejected the same
  way.

## Alternatives considered

- **`golang.org/x/crypto/ssh`, implementing the SSH protocol directly.**
  Rejected — see "Zero new dependencies" above. The real cost isn't the
  dependency itself; it's everything correct SSH auth actually requires
  (agent forwarding, `ProxyJump`, host-key persistence) that this project
  would then have to build and maintain in parallel with the user's own,
  already-correct `ssh` configuration.
- **`docker/cli/cli/connhelper` as a direct dependency**, instead of
  reproducing its ~150-line mechanism locally. Rejected: `docker/cli` is
  a large module with its own substantial transitive dependency tree for
  one small package's worth of actual logic — reproducing the mechanism
  directly keeps the strict `depguard` allowlist unchanged and the new
  code auditable in one small file.
- **A CI-automatable SSH integration test** (an ephemeral `sshd` + keypair
  in GitHub Actions, connecting to `ssh://$(whoami)@127.0.0.1`).
  Considered a reasonable stretch goal, not required for this to ship
  correctly — real added CI complexity for a second launch pass. The
  unit tests in `sshconn_test.go` (a `cat` stand-in for `ssh`,
  argv-inspection via a fake-`ssh` script) cover the plumbing without any
  real SSH involved; end-to-end correctness against a genuine `sshd` and
  remote `docker` CLI was instead verified manually against a real,
  already-available host as part of landing this change.
