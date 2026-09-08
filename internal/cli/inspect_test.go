package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

func TestInspectCmd_TableOutput(t *testing.T) {
	t.Parallel()

	services := []model.Service{
		{Name: "jellyfin", Group: "Media", URL: "http://10.0.0.5:8096", Source: model.Source{Host: "homelab-1"}},
		{Name: "internal-svc", Group: "Other", Source: model.Source{Host: "homelab-1"}},
	}

	cmd := newInspectCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "jellyfin") || !strings.Contains(out, "http://10.0.0.5:8096") {
		t.Errorf("table output is missing jellyfin's row:\n%s", out)
	}
	if !strings.Contains(out, "internal-svc") || !strings.Contains(out, "-") {
		t.Errorf("table output should show \"-\" for a service with no URL:\n%s", out)
	}
}

func TestInspectCmd_TableOutput_Empty(t *testing.T) {
	t.Parallel()

	cmd := newInspectCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return nil, nil, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	if !strings.Contains(stdout.String(), "dashsync.enable") {
		t.Errorf("empty result should explain the opt-in label, got:\n%s", stdout.String())
	}
}

func TestInspectCmd_JSONOutput(t *testing.T) {
	t.Parallel()

	services := []model.Service{
		{ID: "abc123", Name: "jellyfin", Group: "Media", URL: "http://10.0.0.5:8096"},
	}

	cmd := newInspectCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return services, nil, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--output", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	var got []model.Service
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output did not parse as JSON: %v\noutput: %s", err, stdout.String())
	}
	if diff := cmp.Diff(services, got); diff != "" {
		t.Errorf("json output mismatch (-want +got):\n%s", diff)
	}
}

func TestInspectCmd_UnknownOutputFlag(t *testing.T) {
	t.Parallel()

	cmd := newInspectCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return nil, nil, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output", "xml"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() = nil, want an error for an unsupported --output value")
	}
	if !strings.Contains(err.Error(), "xml") {
		t.Errorf("error = %q, want it to mention the passed value %q", err.Error(), "xml")
	}
}

func TestInspectCmd_DiscoveryError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connect to docker: no such host")
	cmd := newInspectCmd(func(context.Context, string, string) ([]model.Service, []error, error) { return nil, nil, wantErr })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(nil)

	err := cmd.Execute()
	if !errors.Is(err, wantErr) {
		t.Errorf("Execute() error = %v, want it to be (or wrap) %v", err, wantErr)
	}
}

func TestInspectCmd_PassesHostAddrFlag(t *testing.T) {
	t.Parallel()

	var gotHostAddr string
	cmd := newInspectCmd(func(_ context.Context, hostAddr, _ string) ([]model.Service, []error, error) {
		gotHostAddr = hostAddr
		return nil, nil, nil
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--host-addr", "10.0.0.5"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	if gotHostAddr != "10.0.0.5" {
		t.Errorf("discover was called with hostAddr %q, want %q", gotHostAddr, "10.0.0.5")
	}
}

func TestInspectCmd_PassesConfigFlag(t *testing.T) {
	t.Parallel()

	var gotConfigPath string
	cmd := newInspectCmd(func(_ context.Context, _, configPath string) ([]model.Service, []error, error) {
		gotConfigPath = configPath
		return nil, nil, nil
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--config", "/etc/dashsync/hosts.yaml"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	if gotConfigPath != "/etc/dashsync/hosts.yaml" {
		t.Errorf("discover was called with configPath %q, want %q", gotConfigPath, "/etc/dashsync/hosts.yaml")
	}
}

func TestInspectCmd_PrintsPerHostWarningsToStderr(t *testing.T) {
	t.Parallel()

	services := []model.Service{{Name: "jellyfin", Group: "Media"}}
	warnings := []error{errors.New(`host "flaky": connection refused`)}

	cmd := newInspectCmd(func(context.Context, string, string) ([]model.Service, []error, error) {
		return services, warnings, nil
	})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil — a warning must not fail the command", err)
	}
	if !strings.Contains(stderr.String(), "flaky") {
		t.Errorf("stderr = %q, want the per-host warning printed", stderr.String())
	}
	if !strings.Contains(stdout.String(), "jellyfin") {
		t.Errorf("stdout = %q, want the services from the hosts that did succeed", stdout.String())
	}
}
