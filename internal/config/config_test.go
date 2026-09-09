package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/NikitaMikhailov/dashsync/internal/config"
)

func TestLoad_MissingFileReturnsDefault(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if diff := cmp.Diff(config.Default(), got); diff != "" {
		t.Errorf("Load() mismatch (-want +got):\n%s", diff)
	}
}

func TestLoad_ParsesHosts(t *testing.T) {
	t.Parallel()

	src := `hosts:
  - name: local
  - name: homelab-2
    address: tcp://10.0.0.6:2376
    url_host: 10.0.0.6
    tls:
      ca: /certs/ca.pem
      cert: /certs/cert.pem
      key: /certs/key.pem
`
	path := writeFile(t, src)

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	want := config.Config{
		Hosts: []config.Host{
			{Name: "local"},
			{
				Name:    "homelab-2",
				Address: "tcp://10.0.0.6:2376",
				URLHost: "10.0.0.6",
				TLS:     &config.TLS{CA: "/certs/ca.pem", Cert: "/certs/cert.pem", Key: "/certs/key.pem"},
			},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load() mismatch (-want +got):\n%s", diff)
	}
}

func TestLoad_RejectsEmptyHostList(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "hosts: []\n")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error for an empty host list")
	}
	if !strings.Contains(err.Error(), "at least one host") {
		t.Errorf("error = %q, want it to explain at least one host is required", err)
	}
}

func TestLoad_RejectsUnnamedHost(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "hosts:\n  - address: tcp://10.0.0.6:2376\n")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error for a host with no name")
	}
	if !strings.Contains(err.Error(), "hosts[0]") {
		t.Errorf("error = %q, want it to name the offending index", err)
	}
}

func TestLoad_RejectsDuplicateHostNames(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "hosts:\n  - name: a\n  - name: a\n")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error for duplicate host names")
	}
	if !strings.Contains(err.Error(), `"a"`) {
		t.Errorf("error = %q, want it to name the duplicated host", err)
	}
}

func TestLoad_RejectsDuplicateHostNamesCaseInsensitively(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "hosts:\n  - name: local\n  - name: Local\n")

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error for names differing only by case")
	}
}

func TestLoad_RejectsTwoHostsWithTheSameAddress(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "hosts:\n  - name: a\n    address: tcp://10.0.0.6:2376\n  - name: b\n    address: tcp://10.0.0.6:2376\n")

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error for two hosts sharing an address")
	}
}

func TestLoad_RejectsTwoHostsBothWithNoAddress(t *testing.T) {
	t.Parallel()

	// Both hosts left address empty means both mean "the environment's own
	// Docker connection" — the same daemon under two names.
	path := writeFile(t, "hosts:\n  - name: a\n  - name: b\n")

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error for two hosts that both default to the local daemon")
	}
}

func TestLoad_RejectsMalformedYAML(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "hosts: [this is not valid\n")

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want a parse error")
	}
}

func TestLoad_RejectsUnknownField(t *testing.T) {
	t.Parallel()

	// A misspelled key must not be silently ignored: that would leave
	// Address empty, pointing this host at the local daemon instead of
	// whatever the operator actually wrote.
	path := writeFile(t, "hosts:\n  - name: a\n    adress: tcp://10.0.0.6:2376\n") //nolint:misspell // intentional: the typo being tested

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error for an unknown field")
	}
}

func TestLoad_EmptyFileIsNotTreatedAsMissing(t *testing.T) {
	t.Parallel()

	// An empty-but-present file is a config mistake, not "no config" — it
	// must not be silently treated the same as a missing file.
	path := writeFile(t, "")

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error: an empty file has no hosts")
	}
}

func TestLoad_RejectsUnreadableFile(t *testing.T) {
	t.Parallel()

	// A directory can be os.ReadFile'd as a path but never successfully
	// read — this must surface as a wrapped error, not os.ErrNotExist's
	// silent-default path.
	dir := t.TempDir()

	_, err := config.Load(dir)
	if err == nil {
		t.Fatal("Load() error = nil, want an error for a path that isn't a regular file")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Errorf("error = %v, want something other than ErrNotExist for a directory", err)
	}
}

func TestLoad_RejectsAddressWithoutScheme(t *testing.T) {
	t.Parallel()

	// A plausible copy-paste from $DOCKER_HOST=10.0.0.6:2376, which is
	// valid there but not a well-formed address here — must be rejected
	// at load time, not silently misinterpreted downstream.
	path := writeFile(t, "hosts:\n  - name: a\n    address: 10.0.0.6:2376\n")

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error for a scheme-less address")
	}
}

func TestLoad_RejectsAddressWithUnsupportedScheme(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "hosts:\n  - name: a\n    address: http://10.0.0.6:2376\n")

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error for an unsupported scheme")
	}
}

func TestLoad_RejectsTLSWithoutAddress(t *testing.T) {
	t.Parallel()

	src := "hosts:\n  - name: a\n    tls:\n      ca: /certs/ca.pem\n"
	path := writeFile(t, src)

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error: tls without an address is silently ignored downstream")
	}
	if !strings.Contains(err.Error(), "tls") {
		t.Errorf("error = %q, want it to mention tls", err)
	}
}

func TestLoad_RejectsTLSOnUnixSocket(t *testing.T) {
	t.Parallel()

	src := "hosts:\n  - name: a\n    address: unix:///var/run/docker.sock\n    tls:\n      ca: /certs/ca.pem\n"
	path := writeFile(t, src)

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error: tls is not meaningful for a unix socket")
	}
}

func TestLoad_AcceptsSSHScheme(t *testing.T) {
	t.Parallel()

	// ssh:// used to be rejected here because github.com/moby/moby/client
	// doesn't implement SSH transport on its own. It's supported now via
	// internal/discovery's own exec+"docker system dial-stdio" mechanism
	// (see docs/decisions/009-ssh-docker-discovery.md) — config validation
	// no longer has any reason to reject it.
	src := "hosts:\n  - name: a\n    address: ssh://user@10.0.0.6\n"
	path := writeFile(t, src)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil: ssh:// is now a supported scheme", err)
	}
	if len(cfg.Hosts) != 1 || cfg.Hosts[0].Address != "ssh://user@10.0.0.6" {
		t.Errorf("Hosts = %+v, want the ssh:// address preserved", cfg.Hosts)
	}
}

func TestLoad_RejectsTLSOnSSHAddress(t *testing.T) {
	t.Parallel()

	src := "hosts:\n  - name: a\n    address: ssh://user@10.0.0.6\n    tls:\n      ca: /certs/ca.pem\n"
	path := writeFile(t, src)

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error: tls is not meaningful over ssh")
	}
	if !strings.Contains(err.Error(), "ssh") {
		t.Errorf("error = %q, want it to mention ssh", err)
	}
}

func TestLoad_RejectsTwoHostsWithDifferentlyCasedSameAddress(t *testing.T) {
	t.Parallel()

	// "TCP://" and "tcp://" (and a trailing slash) name the identical
	// daemon — the duplicate check must compare canonically, not as raw
	// strings, or this reintroduces the double-enumeration bug it exists
	// to prevent.
	src := "hosts:\n  - name: a\n    address: TCP://10.0.0.6:2376\n  - name: b\n    address: tcp://10.0.0.6:2376/\n"
	path := writeFile(t, src)

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error for addresses that are the same daemon once normalized")
	}
}

func TestLoad_TLSViolationPriority_EmptyAddressWinsOverCertKeyMismatch(t *testing.T) {
	t.Parallel()

	// A host breaking two tls rules at once (no address, and a
	// cert/key mismatch) must report the empty-address problem — that's
	// the documented, fixed priority order in validate(), not incidental
	// to how the checks happen to be listed.
	src := "hosts:\n  - name: a\n    tls:\n      cert: /certs/cert.pem\n"
	path := writeFile(t, src)

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "explicit address") {
		t.Errorf("error = %q, want the empty-address violation to be reported first", err)
	}
}

func TestLoad_AcceptsCAOnlyTLS(t *testing.T) {
	t.Parallel()

	// Server-verification without a client cert (no mTLS) is a legitimate
	// configuration client.WithTLSClientConfig itself accepts.
	src := "hosts:\n  - name: a\n    address: tcp://10.0.0.6:2376\n    tls:\n      ca: /certs/ca.pem\n"
	path := writeFile(t, src)

	if _, err := config.Load(path); err != nil {
		t.Errorf("Load() error = %v, want nil for a CA-only tls block", err)
	}
}

func TestLoad_RejectsTLSCertWithoutKey(t *testing.T) {
	t.Parallel()

	src := "hosts:\n  - name: a\n    address: tcp://10.0.0.6:2376\n    tls:\n      cert: /certs/cert.pem\n"
	path := writeFile(t, src)

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error: cert without key fails deep inside the Docker client instead")
	}
	if !strings.Contains(err.Error(), "cert") || !strings.Contains(err.Error(), "key") {
		t.Errorf("error = %q, want it to mention both cert and key", err)
	}
}

func TestLoad_RejectsTLSKeyWithoutCert(t *testing.T) {
	t.Parallel()

	src := "hosts:\n  - name: a\n    address: tcp://10.0.0.6:2376\n    tls:\n      key: /certs/key.pem\n"
	path := writeFile(t, src)

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error for key without cert")
	}
}

func TestHost_ResolveURLHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		host     config.Host
		fallback string
		want     string
	}{
		{
			name:     "explicit url_host wins over everything",
			host:     config.Host{Address: "tcp://10.0.0.6:2376", URLHost: "override.example"},
			fallback: "localhost",
			want:     "override.example",
		},
		{
			name:     "falls back to the hostname parsed from address",
			host:     config.Host{Address: "tcp://10.0.0.6:2376"},
			fallback: "localhost",
			want:     "10.0.0.6",
		},
		{
			name:     "a unix socket address has no hostname to extract",
			host:     config.Host{Address: "unix:///var/run/docker.sock"},
			fallback: "localhost",
			want:     "localhost",
		},
		{
			name:     "no address at all falls back too",
			host:     config.Host{},
			fallback: "localhost",
			want:     "localhost",
		},
		{
			// ResolveURLHost doesn't assume validate() already ran — a
			// Host can be built by hand, bypassing Load entirely — so a
			// malformed address must degrade to fallback rather than
			// panicking or propagating a parse error with no way to
			// return one from this signature.
			name:     "a malformed address falls back without erroring",
			host:     config.Host{Address: "not a valid url at all: []"},
			fallback: "localhost",
			want:     "localhost",
		},
		{
			name:     "url_host still wins over a malformed address",
			host:     config.Host{Address: "not a valid url at all: []", URLHost: "override.example"},
			fallback: "localhost",
			want:     "override.example",
		},
		{
			// A scheme-less address (e.g. copy-pasted from
			// $DOCKER_HOST=10.0.0.6:2376, valid there but not here) fails
			// to parse a hostname out of, same as any other malformed
			// address — validate() is what's expected to catch this one
			// before it ever reaches ResolveURLHost.
			name:     "a scheme-less address has no hostname to extract either",
			host:     config.Host{Address: "10.0.0.6:2376"},
			fallback: "localhost",
			want:     "localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.host.ResolveURLHost(tt.fallback); got != tt.want {
				t.Errorf("ResolveURLHost(%q) = %q, want %q", tt.fallback, got, tt.want)
			}
		})
	}
}

func writeFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "dashsync.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}
