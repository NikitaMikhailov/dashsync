package discovery

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

func TestParseSSHTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		address string
		want    sshTarget
		wantErr bool
	}{
		{name: "user and host", address: "ssh://alice@10.0.0.6", want: sshTarget{User: "alice", Host: "10.0.0.6"}},
		{name: "host only", address: "ssh://10.0.0.6", want: sshTarget{Host: "10.0.0.6"}},
		{name: "user host and port", address: "ssh://alice@10.0.0.6:2222", want: sshTarget{User: "alice", Host: "10.0.0.6", Port: "2222"}},
		{name: "bracketed ipv6", address: "ssh://[2001:db8::1]:22", want: sshTarget{Host: "2001:db8::1", Port: "22"}},
		{name: "empty host is an error", address: "ssh://", wantErr: true},
		{name: "invalid url is an error", address: "ssh://%zz", wantErr: true},
		{name: "password is silently discarded, not carried through", address: "ssh://alice:hunter2@10.0.0.6", want: sshTarget{User: "alice", Host: "10.0.0.6"}},
		{name: "path component is rejected, not silently ignored", address: "ssh://10.0.0.6/var/run/docker.sock", wantErr: true},
		{name: "bare trailing slash is fine (not a real path)", address: "ssh://10.0.0.6/", want: sshTarget{Host: "10.0.0.6"}},

		// Regression: an earlier version of parseSSHTarget passed User/Host
		// straight through unchecked. "ssh://-oProxyCommand=...@host"
		// parses as a valid URL whose userinfo starts with "-" — ssh(1)
		// accepts a bundled short option in that exact shape
		// (-oKey=value) as its first positional argument, and OpenSSH
		// runs a ProxyCommand via the shell before any network connection
		// is attempted. This was exploited for real against the actual
		// system ssh binary (a PoC that created a marker file on disk)
		// before this rejection existed — see the doc comment above.
		{name: "leading dash in userinfo is rejected (option-injection attempt)", address: "ssh://-oProxyCommand=touch%20pwned@10.0.0.6", wantErr: true},
		{name: "leading dash in host is rejected (option-injection attempt)", address: "ssh://-oProxyCommand=touch%20pwned", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseSSHTarget(tc.address)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseSSHTarget(%q) error = nil, want an error", tc.address)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSSHTarget(%q) error = %v, want nil", tc.address, err)
			}
			if got != tc.want {
				t.Errorf("parseSSHTarget(%q) = %+v, want %+v", tc.address, got, tc.want)
			}
		})
	}
}

func TestSSHTarget_Args(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target sshTarget
		want   []string
	}{
		{
			name:   "user host and port",
			target: sshTarget{User: "alice", Host: "10.0.0.6", Port: "2222"},
			want:   []string{"-o", "BatchMode=yes", "-p", "2222", "--", "alice@10.0.0.6", "docker", "system", "dial-stdio"},
		},
		{
			name:   "host only, no port",
			target: sshTarget{Host: "10.0.0.6"},
			want:   []string{"-o", "BatchMode=yes", "--", "10.0.0.6", "docker", "system", "dial-stdio"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.target.args()
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("args() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCommandConn_ReadWriteRoundTrip(t *testing.T) {
	t.Parallel()

	// "cat" stands in for "ssh ... docker system dial-stdio": whatever's
	// written to stdin comes back out stdout, proving the pipe plumbing
	// works without any SSH involved at all.
	conn, err := newCommandConn(exec.CommandContext(t.Context(), "cat"))
	if err != nil {
		t.Fatalf("newCommandConn() error = %v, want nil", err)
	}
	defer conn.Close() //nolint:errcheck // best-effort cleanup

	want := []byte("hello dashsync")
	if _, err := conn.Write(want); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	got := make([]byte, len(want))
	if _, err := readFull(conn, got); err != nil {
		t.Fatalf("Read() error = %v, want nil", err)
	}
	if string(got) != string(want) {
		t.Errorf("round-tripped %q, want %q", got, want)
	}
}

func TestCommandConn_CloseInterruptsBlockedRead(t *testing.T) {
	t.Parallel()

	conn, err := newCommandConn(exec.CommandContext(t.Context(), "cat"))
	if err != nil {
		t.Fatalf("newCommandConn() error = %v, want nil", err)
	}

	readDone := make(chan error, 1)
	go func() {
		_, err := conn.Read(make([]byte, 1))
		readDone <- err
	}()

	// Give the goroutine a moment to actually block in Read before closing
	// — a Close that races a Read that hasn't started yet wouldn't prove
	// anything about interrupting a genuinely blocked one.
	time.Sleep(50 * time.Millisecond)

	if err := conn.Close(); err != nil {
		// cmd.Wait() on a killed process reports a non-zero exit, which
		// is expected here, not a test failure — only a Close that never
		// returns at all would be.
		t.Logf("Close() = %v (expected: the process was killed)", err)
	}

	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Read() did not return after Close(), want it interrupted promptly")
	}

	// ProcessState.Exited() reports false for a process killed by a
	// signal (as Close's Kill() does) — that's expected, not a failure.
	// ProcessState being non-nil at all is what proves Wait() actually
	// ran and reaped the process, leaving no zombie behind.
	if conn.cmd.ProcessState == nil {
		t.Error("process was not reaped after Close(), want no zombie left behind")
	}
}

func TestCommandConn_SurfacesStderrOnFailure(t *testing.T) {
	t.Parallel()

	conn, err := newCommandConn(exec.CommandContext(t.Context(), "sh", "-c", "echo boom >&2; exit 1"))
	if err != nil {
		t.Fatalf("newCommandConn() error = %v, want nil", err)
	}
	defer conn.Close() //nolint:errcheck

	// The command exits immediately, so a Read against its now-closed
	// stdout should fail — with "boom" folded in from stderr.
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Fatal("Read() error = nil, want an error once the process exits without writing anything")
	}
	if !strings.Contains(readErr.Error(), "boom") {
		t.Errorf("Read() error = %v, want it to mention the captured stderr (\"boom\")", readErr)
	}
}

func TestSSHDockerDialer_BuildsExpectedCommand(t *testing.T) {
	t.Parallel()

	// A tiny script standing in for "ssh": echoes its own argv, one per
	// line, to stdout, then exits — proof of exactly what sshDockerDialer
	// invoked, without needing a real ssh binary or server.
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-ssh.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nfor a in \"$@\"; do echo \"$a\"; done\n"), 0o700); err != nil { //nolint:gosec // test fixture, deliberately executable
		t.Fatalf("write fake ssh script: %v", err)
	}

	dial := sshDockerDialer(script, sshTarget{User: "alice", Host: "10.0.0.6", Port: "2222"})
	conn, err := dial(context.Background(), "unused", "unused")
	if err != nil {
		t.Fatalf("dial() error = %v, want nil", err)
	}
	defer conn.Close() //nolint:errcheck

	out := make([]byte, 256)
	n, _ := readUntilEOF(conn, out)
	got := strings.Fields(strings.TrimSpace(string(out[:n])))
	want := []string{"-o", "BatchMode=yes", "-p", "2222", "--", "alice@10.0.0.6", "docker", "system", "dial-stdio"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("invoked ssh with %q, want %q", got, want)
	}
}

func TestNewDockerClientWithDialer_IgnoresHTTPProxy(t *testing.T) {
	// Not t.Parallel(): t.Setenv forbids it.
	t.Setenv("HTTP_PROXY", "http://proxy.example:8080")
	t.Setenv("http_proxy", "http://proxy.example:8080")

	// Regression for a bug found in review: client.WithHost configures
	// the *original* transport's Proxy field as a side effect
	// (sockets.ConfigureTransport's default case sets it to
	// http.ProxyFromEnvironment) before client.WithHTTPClient replaces
	// that transport wholesale — but only client.WithDialContext, not
	// WithHTTPClient, was tried first, and WithDialContext alone leaves
	// that Proxy setting on the original transport in place if it's the
	// one still in use. With $HTTP_PROXY set, every request would
	// silently switch to proxy (absolute-URI) request-line form, which a
	// plain Docker daemon doesn't understand. newDockerClientWithDialer
	// uses WithHTTPClient specifically to replace the transport outright
	// with one whose Proxy is left at its zero value (nil) — this test
	// proves that by inspecting the actual bytes written for a real
	// request, not by asserting an internal field.
	serverEnd, clientEnd := net.Pipe()
	captured := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _ := serverEnd.Read(buf)
		captured <- string(buf[:n])
	}()

	dial := func(context.Context, string, string) (net.Conn, error) { return clientEnd, nil }
	c, err := newDockerClientWithDialer(dial)
	if err != nil {
		t.Fatalf("newDockerClientWithDialer() error = %v, want nil", err)
	}
	defer c.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, _ = c.Ping(ctx, client.PingOptions{}) // the fake server never answers; only the request line matters here

	select {
	case reqLine := <-captured:
		if strings.Contains(reqLine, "http://"+client.DummyHost) {
			t.Errorf("request line used proxy absolute-URI form despite $HTTP_PROXY being set: %q", reqLine)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no request observed on the fake connection")
	}
}

// readFull reads exactly len(buf) bytes or fails trying.
func readFull(r interface{ Read([]byte) (int, error) }, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// readUntilEOF reads until the reader signals it has nothing more to
// give, for a subprocess whose total output length isn't known upfront.
func readUntilEOF(r interface{ Read([]byte) (int, error) }, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, nil //nolint:nilerr // EOF (or the process exiting) is the expected way this loop ends
		}
	}
	return total, nil
}
