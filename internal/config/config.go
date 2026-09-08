// Package config reads dashsync.yaml — the list of Docker hosts to
// discover from. A missing file is not an error: it means "just the
// local Docker daemon," the same single-host behavior dashsync had before
// multi-host support existed.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// validAddressSchemes are the URL schemes client.WithHost (and, in
// practice, $DOCKER_HOST) accept on the platforms dashsync actually builds
// for (linux, darwin — see .github/workflows/ci.yml). npipe, Windows' named
// pipe transport, is deliberately not here: there's no Windows build to
// exercise it against, and adding it back the day that changes costs one
// line, not a redesign.
var validAddressSchemes = map[string]bool{ //nolint:gochecknoglobals // read-only lookup table, never mutated
	"tcp":  true,
	"unix": true,
	"ssh":  true,
}

// tlsIneffectiveSchemes are address schemes whose connection never
// consults client.WithTLSClientConfig at all: a unix socket has no TLS
// layer, and an ssh:// connection authenticates over SSH instead. A tls:
// block set alongside either would be silently ignored rather than doing
// what it looks like it does.
var tlsIneffectiveSchemes = map[string]bool{ //nolint:gochecknoglobals // read-only lookup table, never mutated
	"unix": true,
	"ssh":  true,
}

// TLS holds the client certificate material for a TCP+TLS Docker
// connection — the standard "dockerd -H tcp://...:2376 --tlsverify" setup.
type TLS struct {
	CA   string `yaml:"ca"`
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

// Host describes one Docker endpoint dashsync should discover from.
type Host struct {
	// Name identifies this host in Service.Source.Host and in dashsync's
	// own output (inspect's HOST column, sync's per-host warnings) — it
	// doesn't need to be a resolvable hostname.
	Name string `yaml:"name"`
	// Address is how to reach the Docker API: "unix:///var/run/docker.sock",
	// "tcp://10.0.0.6:2376", or empty to use the standard DOCKER_HOST
	// environment variable (or the platform default socket if that's
	// unset too) — the same resolution client.FromEnv already does.
	Address string `yaml:"address,omitempty"`
	// URLHost is the host or IP used to build a URL auto-detected from
	// this host's published ports. Defaults to Address's own hostname if
	// set, or the CLI's --host-addr value otherwise — see
	// Host.ResolveURLHost.
	URLHost string `yaml:"url_host,omitempty"`
	TLS     *TLS   `yaml:"tls,omitempty"`
}

// ResolveURLHost returns the host or IP that should be used to build a URL
// auto-detected from this host's published ports: h.URLHost if it's set,
// otherwise the hostname parsed out of h.Address if that's set to
// something with one (a unix socket path has none), otherwise fallback —
// in practice the CLI's own --host-addr default.
func (h Host) ResolveURLHost(fallback string) string {
	if h.URLHost != "" {
		return h.URLHost
	}
	if hostname := addressHostname(h.Address); hostname != "" {
		return hostname
	}
	return fallback
}

// addressHostname parses address and returns its hostname, or "" if it has
// none (a unix socket path) or doesn't parse at all. A Host built by Load
// has already been through validate(), which rejects anything
// addressScheme can't make sense of — but Host is also a plain exported
// struct nothing stops a caller from constructing by hand, so this stays
// defensive rather than assuming validate() always ran first.
func addressHostname(address string) string {
	if address == "" {
		return ""
	}
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// addressScheme parses address and returns its scheme plus a canonical
// form suitable for equality comparison across hosts (lowercased
// scheme/host, trailing slash trimmed) — "TCP://10.0.0.6:2376" and
// "tcp://10.0.0.6:2376/" name the same daemon and must compare equal, not
// just happen to be byte-identical. An empty address returns ("", "", nil):
// it means "use the environment's own Docker connection," which has no
// scheme to check.
//
// This is also the single point deciding whether an address is well-formed
// enough to build a Docker client from later: a scheme-less address like
// "10.0.0.6:2376" (a plausible copy-paste from $DOCKER_HOST, which does
// accept that form) or one with a scheme client.WithHost doesn't understand
// would otherwise pass validate() silently, then either build a URL from
// the wrong host hostname or fail deep inside client construction — far
// from the config line that actually caused it.
func addressScheme(address string) (scheme, canonical string, err error) {
	if address == "" {
		return "", "", nil
	}
	u, err := url.Parse(address)
	if err != nil {
		return "", "", fmt.Errorf("invalid address %q: %w", address, err)
	}
	if !validAddressSchemes[u.Scheme] {
		return "", "", fmt.Errorf("invalid address %q: unsupported scheme %q, want tcp://, unix://, or ssh://",
			address, u.Scheme)
	}
	canonical = strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + strings.TrimSuffix(u.Path, "/")
	return u.Scheme, canonical, nil
}

// Config is dashsync.yaml's top-level shape.
type Config struct {
	Hosts []Host `yaml:"hosts"`
}

// Default returns the single-host configuration dashsync uses when no
// config file exists: one host named "local", connected to via the
// standard Docker environment variables. Every milestone before
// multi-host support existed behaved exactly this way, and a zero-config
// `dashsync sync` needs to keep behaving like that.
func Default() Config {
	return Config{Hosts: []Host{{Name: "local"}}}
}

// Load reads and validates the config file at path. A missing file
// returns Default(), not an error — multi-host support is opt-in.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}

	// DisallowUnknownField turns a typo'd key (e.g. a missing letter in
	// "address") into a parse error instead of a silently-empty field —
	// hand-edited YAML is the only interface this package has, so a typo
	// in an optional field must not be indistinguishable from "not set."
	var cfg Config
	if err := yaml.UnmarshalWithOptions(data, &cfg, yaml.DisallowUnknownField()); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	if len(c.Hosts) == 0 {
		return errors.New(`at least one host is required under "hosts"`)
	}

	seenNames := make(map[string]bool, len(c.Hosts))
	seenAddresses := make(map[string]bool, len(c.Hosts))
	for i, h := range c.Hosts {
		if h.Name == "" {
			return fmt.Errorf("hosts[%d] has no name", i)
		}
		// Compared case-insensitively: "local" and "Local" would be
		// indistinguishable in inspect's HOST column and in
		// Service.Source.Host, so treating them as distinct names would
		// just be a confusing way to fail later instead of clearly now.
		nameKey := strings.ToLower(h.Name)
		if seenNames[nameKey] {
			return fmt.Errorf("duplicate host name %q", h.Name)
		}
		seenNames[nameKey] = true

		scheme, canonicalAddress, err := addressScheme(h.Address)
		if err != nil {
			return fmt.Errorf("hosts[%d] (%q): %w", i, h.Name, err)
		}

		// Two hosts resolving to the same daemon (including two hosts both
		// left empty, which both mean "the environment's own Docker
		// connection") would enumerate its containers twice under two
		// different Host.Name values in the merged output — exactly the
		// kind of duplication multi-host support must not introduce.
		// Compared on addressScheme's canonical form, not the raw string:
		// "TCP://10.0.0.6:2376" and "tcp://10.0.0.6:2376/" name the same
		// daemon and must be caught as duplicates too.
		if seenAddresses[canonicalAddress] {
			label := h.Address
			if label == "" {
				label = "the default local Docker connection"
			}
			return fmt.Errorf("hosts[%d] (%q): another host already uses %s", i, h.Name, label)
		}
		seenAddresses[canonicalAddress] = true

		// Checked in a fixed order so which error wins when a host breaks
		// more than one of these rules at once is a deliberate choice, not
		// however these three happen to be listed: an empty address makes
		// tls meaningless regardless of scheme, so that's reported first;
		// a scheme that ignores tls entirely comes before a mere
		// cert/key mismatch, which is the least surprising of the three.
		if h.TLS != nil {
			switch {
			case h.Address == "":
				return fmt.Errorf(
					"hosts[%d] (%q): tls has no effect without an explicit address "+
						"(an empty address means the environment's own Docker connection, "+
						"which doesn't consult this config's tls settings)", i, h.Name)
			case tlsIneffectiveSchemes[scheme]:
				return fmt.Errorf("hosts[%d] (%q): tls has no effect on a %s:// address", i, h.Name, scheme)
			case (h.TLS.Cert == "") != (h.TLS.Key == ""):
				return fmt.Errorf("hosts[%d] (%q): tls.cert and tls.key must both be set or both left empty", i, h.Name)
			}
		}
	}
	return nil
}
