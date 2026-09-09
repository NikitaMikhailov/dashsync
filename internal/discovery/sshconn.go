package discovery

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/client"
)

// sshTarget is a parsed "ssh://[user@]host[:port]" address's connection
// parts. Pure and side-effect-free — no exec, no network — so it's
// unit-testable on its own.
type sshTarget struct {
	User, Host, Port string
}

// parseSSHTarget parses an "ssh://" address. Only the user/host/port ever
// matter here — see docs/decisions/009-ssh-docker-discovery.md for why
// everything else (identity file, ProxyJump, agent, host-key checking)
// is deliberately left to the caller's own ssh(1) configuration rather
// than a dashsync config field: there's exactly one reasonable place for
// each of those to live already, and it isn't here.
func parseSSHTarget(address string) (sshTarget, error) {
	u, err := url.Parse(address)
	if err != nil {
		return sshTarget{}, fmt.Errorf("invalid ssh address %q: %w", address, err)
	}
	if u.Hostname() == "" {
		return sshTarget{}, fmt.Errorf("invalid ssh address %q: no host", address)
	}
	var user string
	if u.User != nil {
		user = u.User.Username()
	}
	return sshTarget{User: user, Host: u.Hostname(), Port: u.Port()}, nil
}

// args returns the ssh(1) CLI arguments identifying this target and
// requesting a non-interactive session: BatchMode=yes means a host whose
// key isn't already known fails fast with a clear stderr message instead
// of hanging on a prompt no discovery run can ever answer — the first
// connection to a new host has to be accepted with a manual `ssh` run
// once, same as Docker's own ssh:// support (see the ADR and README).
func (t sshTarget) args() []string {
	args := []string{"-o", "BatchMode=yes"}
	if t.Port != "" {
		args = append(args, "-p", t.Port)
	}
	host := t.Host
	if t.User != "" {
		host = t.User + "@" + host
	}
	return append(args, host, "docker", "system", "dial-stdio")
}

// commandConn adapts a subprocess's stdin/stdout pipes to net.Conn, the
// shape client.WithDialContext needs. Modeled on the same mechanism
// docker/cli's own ssh:// support uses (exec ssh running
// "docker system dial-stdio" on the far end) rather than vendoring an SSH
// client library — see docs/decisions/009-ssh-docker-discovery.md.
type commandConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr *bytes.Buffer

	// waitOnce guards cmd.Wait() specifically: exec.Cmd documents calling
	// Wait twice as an error, and both a Read that just observed the
	// process exit and a concurrent Close() can plausibly reach it first.
	waitOnce sync.Once
	waitErr  error
}

// newCommandConn starts cmd with its stdin/stdout wired up as a conn and
// stderr captured for error context — ssh's own diagnostics ("Host key
// verification failed", "Permission denied (publickey)", "docker: command
// not found") are exactly what an operator needs on failure, and none of
// it would otherwise reach the caller.
func newCommandConn(cmd *exec.Cmd) (*commandConn, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	return &commandConn{cmd: cmd, stdin: stdin, stdout: stdout, stderr: &stderr}, nil
}

func (c *commandConn) Read(p []byte) (int, error) {
	n, err := c.stdout.Read(p)
	if err != nil {
		err = c.annotate(err)
	}
	return n, err
}

func (c *commandConn) Write(p []byte) (int, error) {
	n, err := c.stdin.Write(p)
	if err != nil {
		err = c.annotate(err)
	}
	return n, err
}

// annotate folds captured stderr into err, if there is any — a plain
// "broken pipe" or "exit status 255" (or a bare io.EOF, if the process
// exited without writing anything) tells an operator nothing about *why*
// the ssh session ended.
//
// wait() runs first, unconditionally: cmd.Stderr is drained by a
// goroutine exec.Cmd itself starts on Start(), and that goroutine
// finishing is only guaranteed once Wait() returns — reading stdout's
// EOF (the child closed its output) races that goroutine's last write
// into the stderr buffer, so annotate must block until Wait() confirms
// the process is fully done and every byte of stderr it produced is
// captured, or it can miss the very message it exists to surface.
func (c *commandConn) annotate(err error) error {
	_ = c.wait() // its result isn't the error being annotated; only stderr having drained matters here
	if msg := strings.TrimSpace(c.stderr.String()); msg != "" {
		return fmt.Errorf("%w: %s", err, msg)
	}
	return err
}

// wait calls cmd.Wait() exactly once, however many of Read, Write, and
// Close race to trigger it — exec.Cmd documents a second Wait() call as
// an error. This project's own usage never has more than one reader in
// flight at a time (net/http's transport drives one conn from one
// goroutine at a time), so calling it as soon as any one of them
// observes the process is done doesn't run afoul of exec.Cmd's "don't
// Wait before every read has completed" rule in practice.
func (c *commandConn) wait() error {
	c.waitOnce.Do(func() {
		c.waitErr = c.cmd.Wait()
	})
	return c.waitErr
}

// Close kills the process, then waits for it — per exec.Cmd's own
// documented pipe-lifecycle contract, Wait (not a manual stdin/stdout
// Close) is what actually closes the pipes once the process has exited.
// Kill first unblocks a Read/Write that's currently stuck on the pipe.
func (c *commandConn) Close() error {
	_ = c.cmd.Process.Kill()
	return c.wait()
}

// LocalAddr, RemoteAddr, and the SetDeadline family exist only to satisfy
// net.Conn — verified against net/http's actual transport source that it
// calls none of the deadline setters on a client connection (context
// cancellation closing the conn, via Close above, is what actually
// enforces a request timeout), and nothing in this project's read-only
// Docker API usage inspects a connection's address.
func (c *commandConn) LocalAddr() net.Addr              { return sshConnAddr{} }
func (c *commandConn) RemoteAddr() net.Addr             { return sshConnAddr{} }
func (c *commandConn) SetDeadline(time.Time) error      { return nil }
func (c *commandConn) SetReadDeadline(time.Time) error  { return nil }
func (c *commandConn) SetWriteDeadline(time.Time) error { return nil }

// sshConnAddr is a trivial net.Addr — there's no real address to report
// for a pipe over an SSH session, only a fixed, honest label.
type sshConnAddr struct{}

func (sshConnAddr) Network() string { return "ssh" }
func (sshConnAddr) String() string  { return "ssh-docker-dial-stdio" }

// sshDockerDialer returns a client.WithDialContext-compatible dial
// function that reaches target's Docker daemon by execing sshBinary to
// run "docker system dial-stdio" over an SSH session. sshBinary is
// parameterized (production code always passes "ssh") so tests can
// substitute a stand-in without a real ssh binary or server.
func sshDockerDialer(sshBinary string, target sshTarget) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		cmd := exec.CommandContext(ctx, sshBinary, target.args()...)
		return newCommandConn(cmd)
	}
}

// newSSHDockerClient builds a *client.Client for an "ssh://" address.
//
// Option order matters and will silently misbehave if reversed:
// client.WithHost's own implementation (sockets.ConfigureTransport)
// overwrites the transport's DialContext with a plain TCP dialer as its
// default-case behavior, so WithDialContext must be listed after WithHost
// to have the last word. client.DummyHost is the SDK's own placeholder
// for "this is never actually resolved" — the custom dialer below ignores
// whatever network/addr the HTTP layer passes it and always reaches the
// same fixed SSH target instead.
func newSSHDockerClient(address string) (*client.Client, error) {
	target, err := parseSSHTarget(address)
	if err != nil {
		return nil, err
	}
	c, err := client.New(
		client.WithHost("http://"+client.DummyHost),
		client.WithDialContext(sshDockerDialer("ssh", target)),
	)
	if err != nil {
		return nil, fmt.Errorf("build ssh docker client: %w", err)
	}
	return c, nil
}
