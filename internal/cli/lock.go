//go:build unix

package cli

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// acquireLock takes an exclusive, non-blocking advisory lock guarding
// outputPath against a second dashsync process reading and writing it at
// the same time — two overlapping cron schedules, or a scheduled run
// overlapping someone testing by hand, would otherwise each read the same
// starting content, compute independent merges, and let whichever writes
// last silently discard the other's changes (the known gap this closes —
// see docs/decisions/002-merge-strategy.md's "No cross-process locking").
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
// The returned release func must be called (typically via defer) once the
// caller is done reading outputPath and, if it's going to, writing it.
// The lock file itself is deliberately never removed — see release's own
// doc comment for why.
func acquireLock(outputPath string) (release func() error, err error) {
	lockPath := outputPath + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", lockPath, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
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
		// safety; it's harmless to commit or .gitignore.
		defer func() { _ = f.Close() }()
		return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}, nil
}
