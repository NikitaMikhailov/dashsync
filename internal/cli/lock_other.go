//go:build !unix

package cli

// acquireLock is a no-op on a non-unix build: dashsync only ever ships
// linux/darwin release binaries (see .goreleaser.yaml), but the package
// itself shouldn't stop compiling on every other GOOS just because the
// real implementation (lock.go, //go:build unix) uses syscall.Flock. No
// locking protection is offered here — a build on an unsupported platform
// was already unsupported before file locking existed; this only keeps
// `go build`/`go vet` working on one, not adds a guarantee to it.
func acquireLock(_ string, _ bool) (release func() error, err error) {
	return func() error { return nil }, nil
}
