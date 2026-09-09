//go:build unix

// This file exercises real flock contention (lock.go's implementation) —
// lock_other.go's non-unix stub has no contention to prove, and letting
// these tests run against it would silently prove nothing while looking
// like they proved something. Skipping the whole file, not individual
// tests, keeps that failure mode from ever needing to be told apart from
// a real regression.
package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireLock_SecondExclusiveCallFailsWhileFirstHoldsExclusive(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")

	release, err := acquireLock(path, true)
	if err != nil {
		t.Fatalf("first acquireLock() = %v, want nil", err)
	}
	t.Cleanup(func() { _ = release() })

	if _, err := acquireLock(path, true); err == nil {
		t.Fatal("second acquireLock(exclusive) on the same path = nil, want an error while the first exclusive lock is held")
	}
}

func TestAcquireLock_SharedCallFailsWhileExclusiveIsHeld(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")

	release, err := acquireLock(path, true)
	if err != nil {
		t.Fatalf("acquireLock(exclusive) = %v, want nil", err)
	}
	t.Cleanup(func() { _ = release() })

	if _, err := acquireLock(path, false); err == nil {
		t.Fatal("acquireLock(shared) while an exclusive lock is held = nil, want an error")
	}
}

func TestAcquireLock_ExclusiveCallFailsWhileSharedIsHeld(t *testing.T) {
	t.Parallel()

	// The direction that matters most in production: a real writer must
	// not proceed while a --check (or --dry-run) run is mid-flight,
	// exactly the scenario a scheduled --check overlapping a scheduled
	// real sync produces. The reverse direction (exclusive blocks shared)
	// is covered by TestAcquireLock_SharedCallFailsWhileExclusiveIsHeld;
	// proving only one direction of this asymmetric pair wouldn't catch a
	// regression that silently swapped LOCK_SH and LOCK_EX in acquireLock.
	path := filepath.Join(t.TempDir(), "services.yaml")

	release, err := acquireLock(path, false)
	if err != nil {
		t.Fatalf("acquireLock(shared) = %v, want nil", err)
	}
	t.Cleanup(func() { _ = release() })

	if _, err := acquireLock(path, true); err == nil {
		t.Fatal("acquireLock(exclusive) while a shared lock is held = nil, want an error")
	}
}

func TestAcquireLock_TwoSharedCallsDoNotContend(t *testing.T) {
	t.Parallel()

	// Two --check (or --dry-run) runs against the same file are both
	// read-only and must not fail each other — only a writer needs
	// exclusivity.
	path := filepath.Join(t.TempDir(), "services.yaml")

	release1, err := acquireLock(path, false)
	if err != nil {
		t.Fatalf("first acquireLock(shared) = %v, want nil", err)
	}
	t.Cleanup(func() { _ = release1() })

	release2, err := acquireLock(path, false)
	if err != nil {
		t.Fatalf("second acquireLock(shared) = %v, want nil: two readers must not block each other", err)
	}
	_ = release2()
}

func TestAcquireLock_ReleaseAllowsReacquiring(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")

	release, err := acquireLock(path, true)
	if err != nil {
		t.Fatalf("first acquireLock() = %v, want nil", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release() = %v, want nil", err)
	}

	release2, err := acquireLock(path, true)
	if err != nil {
		t.Fatalf("acquireLock() after release = %v, want nil", err)
	}
	_ = release2()
}

func TestAcquireLock_CreatesSidecarLockFileNotTheOutputFileItself(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")

	release, err := acquireLock(path, true)
	if err != nil {
		t.Fatalf("acquireLock() = %v, want nil", err)
	}
	t.Cleanup(func() { _ = release() })

	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Errorf("expected a %s.lock sidecar file to exist: %v", path, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("acquireLock() must not create or touch the output file itself")
	}
}

func TestAcquireLock_DifferentPathsDoNotContend(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	release1, err := acquireLock(filepath.Join(dir, "a.yaml"), true)
	if err != nil {
		t.Fatalf("acquireLock(a) = %v, want nil", err)
	}
	t.Cleanup(func() { _ = release1() })

	release2, err := acquireLock(filepath.Join(dir, "b.yaml"), true)
	if err != nil {
		t.Fatalf("acquireLock(b) = %v, want nil: a lock on a.yaml must not block b.yaml", err)
	}
	_ = release2()
}
