package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireLock_SecondCallFailsWhileFirstHoldsIt(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")

	release, err := acquireLock(path)
	if err != nil {
		t.Fatalf("first acquireLock() = %v, want nil", err)
	}
	t.Cleanup(func() { _ = release() })

	if _, err := acquireLock(path); err == nil {
		t.Fatal("second acquireLock() on the same path = nil, want an error while the first lock is held")
	}
}

func TestAcquireLock_ReleaseAllowsReacquiring(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")

	release, err := acquireLock(path)
	if err != nil {
		t.Fatalf("first acquireLock() = %v, want nil", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release() = %v, want nil", err)
	}

	release2, err := acquireLock(path)
	if err != nil {
		t.Fatalf("acquireLock() after release = %v, want nil", err)
	}
	_ = release2()
}

func TestAcquireLock_CreatesSidecarLockFileNotTheOutputFileItself(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "services.yaml")

	release, err := acquireLock(path)
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

	release1, err := acquireLock(filepath.Join(dir, "a.yaml"))
	if err != nil {
		t.Fatalf("acquireLock(a) = %v, want nil", err)
	}
	t.Cleanup(func() { _ = release1() })

	release2, err := acquireLock(filepath.Join(dir, "b.yaml"))
	if err != nil {
		t.Fatalf("acquireLock(b) = %v, want nil: a lock on a.yaml must not block b.yaml", err)
	}
	_ = release2()
}
