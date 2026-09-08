package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/google/go-cmp/cmp"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

func TestSyncCmd_RendersHomepageByDefault(t *testing.T) {
	t.Parallel()

	services := []model.Service{
		{Name: "Jellyfin", Group: "Media", URL: "http://10.0.0.5:8096"},
	}

	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return services, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	want := "- Media:\n  - Jellyfin:\n      href: http://10.0.0.5:8096\n"
	if stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestSyncCmd_GroupsBeforeRendering(t *testing.T) {
	t.Parallel()

	// Scrambled group/name order on the way in — sync must group+sort
	// before handing off to the renderer, not just pass services through.
	services := []model.Service{
		{Name: "sonarr", Group: "Media"},
		{Name: "adguard", Group: "Network"},
		{Name: "jellyfin", Group: "Media"},
	}

	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return services, nil })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	// Decode structurally rather than searching the raw text for
	// substrings: a service's own description or icon could just as
	// easily contain the literal text "Media" or "sonarr" and pass a
	// position-based string search for the wrong reason entirely.
	var groups []map[string][]map[string]any
	if err := yaml.Unmarshal(stdout.Bytes(), &groups); err != nil {
		t.Fatalf("output did not parse as YAML: %v\noutput:\n%s", err, stdout.String())
	}

	gotGroupNames := groupKeys(groups)
	wantGroupNames := []string{"Media", "Network"}
	if diff := cmp.Diff(wantGroupNames, gotGroupNames); diff != "" {
		t.Errorf("group order mismatch (-want +got):\n%s", diff)
	}

	mediaServices := serviceKeys(groups[0]["Media"])
	wantMediaServices := []string{"jellyfin", "sonarr"}
	if diff := cmp.Diff(wantMediaServices, mediaServices); diff != "" {
		t.Errorf("service order within Media mismatch (-want +got):\n%s", diff)
	}
}

func groupKeys(groups []map[string][]map[string]any) []string {
	names := make([]string, len(groups))
	for i, g := range groups {
		for name := range g {
			names[i] = name
		}
	}
	return names
}

func serviceKeys(services []map[string]any) []string {
	names := make([]string, len(services))
	for i, s := range services {
		for name := range s {
			names[i] = name
		}
	}
	return names
}

func TestSyncCmd_UnknownFormat(t *testing.T) {
	t.Parallel()

	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return nil, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--format", "dashy"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() = nil, want an error for an unsupported --format value")
	}
	if !strings.Contains(err.Error(), "dashy") {
		t.Errorf("error = %q, want it to mention the passed value %q", err.Error(), "dashy")
	}
}

func TestSyncCmd_DiscoveryError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connect to docker: no such host")
	cmd := newSyncCmd(func(context.Context, string) ([]model.Service, error) { return nil, wantErr })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(nil)

	if err := cmd.Execute(); !errors.Is(err, wantErr) {
		t.Errorf("Execute() error = %v, want it to be (or wrap) %v", err, wantErr)
	}
}

func TestSyncCmd_PassesHostAddrFlag(t *testing.T) {
	t.Parallel()

	var gotHostAddr string
	cmd := newSyncCmd(func(_ context.Context, hostAddr string) ([]model.Service, error) {
		gotHostAddr = hostAddr
		return nil, nil
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
