# 008 — Cross-process locking for `sync --output-path`

## Context

ADR 002 named this as a known, unfixed gap: two `sync --output-path`
invocations against the same file, overlapping in time (two cron
schedules, a scheduled run overlapping someone testing by hand), each
read the same starting content, compute independent merges, and whichever
writes last silently discards the other's changes — neither run reports a
conflict, because neither ever sees the other.

## Decision

`sync` takes an advisory lock on `<output-path>.lock` — a sidecar file,
never `outputPath` itself — for the whole read-merge-(write) window,
before `readIfExists` and released once the run is done.

**A sidecar file, not `outputPath` directly.** `writeAtomic` replaces
`outputPath` by renaming a temp file over it. `flock`'s exclusion is tied
to the *inode* a lock was taken on, not the path — locking `outputPath`
directly would silently stop protecting anything the instant a write
actually happened, since the next reader opening that path would get a
fresh inode with no memory of the old lock. A lock file that's never
replaced, only ever opened and locked, doesn't have that problem.

**Exclusive for a write, shared for `--check`/`--dry-run`.** Only a run
that's actually going to write needs to keep every other locker out; two
read-only previews (two `--check` runs on overlapping schedules, or a
`--check` racing an in-progress write) don't conflict with each other, and
`writeAtomic`'s rename-based replacement already makes a concurrent read
safe against ever observing a half-written file. Locking every invocation
exclusively would make two harmless concurrent `--check` runs fail each
other for no real reason — caught in review of the first version of this
change, which did exactly that.

**Non-blocking, not waiting for the lock to free up.** dashsync is a
one-shot CLI, not a daemon — there's no good default for how long a wait
should be, and a wait that hangs inside a cron job just makes the *next*
scheduled run's overlap worse, not better. Two overlapping runs against
the same file is a scheduling mistake worth surfacing immediately as a
clear error, not queueing silently.

**The lock file is never deleted.** Unlinking it after use would race a
concurrent `acquireLock` that already opened it under the old inode: that
caller would go on holding a lock nobody else can see once a third process
creates and locks a fresh file at the same path — the classic
flock-then-unlink footgun. An inert `<output-path>.lock` file sitting
next to the real config permanently is the price of avoiding that; add it
to your own `.gitignore` if you don't want to see it in `git status`, it
carries no meaningful content either way.

**`syscall.Flock`, not a third-party locking library.** Zero new
dependencies — `$gostd` is already allowed by depguard, and `syscall.Flock`
exists on every platform dashsync actually ships a release binary for
(linux, darwin — see `.goreleaser.yaml`). A `//go:build !unix` stub keeps
`go build`/`go vet` working on every other `GOOS` (a real regression an
earlier version of this change introduced and review caught: the package
stopped cross-compiling for Windows, even though dashsync never claimed
Windows support to begin with — a platform this tool was never going to
run on shouldn't be one it can't even *compile* for).

## Known limitation, not fixed here

`flock`'s exclusion is only as reliable as the filesystem `outputPath`
lives on. An ordinary local filesystem (ext4, APFS, ...) — the expected
case for a config file a cron job writes to — is fine. A directory
bind-mounted into a container through certain virtualized or network
filesystem layers is not: confirmed directly (two genuinely separate OS
processes, not two calls in one test process) against Docker Desktop's
virtiofs specifically, where a second process acquired an exclusive lock
while the first was still holding it, with no error from either. There's
no portable fix at this layer available to a CLI tool with no daemon and
no external service — a real distributed lock needs infrastructure this
project deliberately doesn't carry (see the top-level README's own
"what it doesn't do"). This protects the ordinary case — dashsync and its
output file sharing a real filesystem — and is best-effort, not a
guarantee, the moment a container boundary and a bind mount sit between
two writers.

## Alternatives considered

- **`O_CREAT|O_EXCL`-based lock-file creation** instead of `flock`.
  Rejected: a crashed process leaves the lock file behind forever, and
  there's no way to tell "still running" from "died mid-run" apart from a
  staleness timeout — which trades one class of footgun (silent data loss
  from no locking at all) for another (a stale lock silently blocking
  every future run until someone notices and deletes a file by hand).
  `flock` releases automatically when the holding process's file
  descriptor closes, crash included, without needing a timeout at all.
- **Blocking with a timeout** instead of failing immediately. Rejected —
  see "Non-blocking" above: there's no good default timeout for a one-shot
  CLI, and this project's established preference throughout (`--dry-run`
  defaulting under `$CI`, `--check`'s distinct exit code) is a clear,
  immediate signal over a wait that might paper over a real scheduling
  problem.

## Consequences

- A `<output-path>.lock` file now appears next to every file `sync
  --output-path` has ever written to, permanently. Harmless, but visible
  in `git status` if that directory is a git repo — worth a line in your
  own `.gitignore`.
- Two overlapping writers now fail loudly and immediately instead of one
  silently losing its changes — the entire point, but it does mean a
  scheduling mistake that used to go unnoticed now surfaces as a sync
  failure, which is a behavior change for anyone who (perhaps
  unknowingly) already had overlapping schedules.
