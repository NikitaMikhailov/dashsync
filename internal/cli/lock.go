//go:build unix

package cli

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// acquireLock takes a non-blocking advisory lock guarding outputPath
// against a second dashsync process reading and writing it at the same
// time — two overlapping cron schedules, or a scheduled run overlapping
// someone testing by hand, would otherwise each read the same starting
// content, compute independent merges, and let whichever writes last
// silently discard the other's changes (the known gap this closes — see
// docs/decisions/002-merge-strategy.md's "No cross-process locking").
//
// exclusive distinguishes a writer from a --check-only reader: two
// concurrent --check runs (or a --check racing an actual write, which
// writeAtomic's own rename-based replacement already makes safe to
// observe mid-write) don't conflict with each other and shouldn't fail
// just because both happened to land on the same schedule — only two
// writers, or a writer and anyone else, actually need to be kept apart.
// Pass true from the write path, false from --check's read-only one.
//
// The lock lives on a sidecar file, outputPath+".lock", never on
// outputPath itself: writeAtomic replaces outputPath by renaming a temp
// file over it, and renaming over a file doesn't disturb a lock held on
// a *different* open file description pointing at outputPath's old
// inode — flock's guarantee is tied to the inode a lock was taken on, not
// the path, so locking outputPath directly would silently stop protecting
// anything the moment a write actually happens. A lock file that's never
// replaced, only ever opened and locked, doesn't have that problem.
//
// Non-blocking rather than waiting for the lock to free up: dashsync is a
// one-shot CLI, not a daemon, so there's no good default for how long a
// wait should be, and a wait that hangs in a cron job just makes the
// *next* scheduled run's overlap worse, not better. Two overlapping runs
// against the same file is a scheduling mistake worth surfacing
// immediately, not queueing silently.
//
// Known limitation, not fixed here: flock's exclusion is only as reliable
// as the filesystem outputPath lives on. A genuine local filesystem (ext4,
// APFS, ...) — the ordinary case for a config file cron writes to — is
// fine. A directory bind-mounted into a container through certain
// virtualized/network filesystem layers (confirmed against Docker
// Desktop's virtiofs specifically) can silently let two separate
// processes each believe they hold an exclusive lock at once, with no
// error from either. There's no portable fix at this layer — a
// distributed lock service is real infrastructure this CLI deliberately
// doesn't carry — so this protects the ordinary case (dashsync and its
// output file on the same real filesystem) and is best-effort, not a
// guarantee, the moment a container boundary and a bind mount are
// involved.
//
// The returned release func must be called (typically via defer) once the
// caller is done reading outputPath and, if it's going to, writing it.
// The lock file itself is deliberately never removed — see release's own
// doc comment for why.
func acquireLock(outputPath string, exclusive bool) (release func() error, err error) {
	lockPath := outputPath + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", lockPath, err)
	}

	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%s is locked by another dashsync sync writing to it right now", outputPath)
		}
		return nil, fmt.Errorf("lock %s: %w", lockPath, err)
	}

	return func() error {
		// The lock file is left in place, empty, permanently — deleting it
		// would race a concurrent acquireLock that already opened it under
		// the old inode: that caller would go on holding a lock nobody else
		// can see once a third process creates and locks a fresh file at
		// the same path, defeating the exclusion this whole mechanism
		// exists for. An inert <output>.lock file is the price of that
		// safety; add it to your own .gitignore, it carries no meaningful
		// content.
		defer func() { _ = f.Close() }()
		return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}, nil
}
