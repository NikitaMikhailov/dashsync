package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewRootCmd_RegistersSubcommands(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()

	registered := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		registered[sub.Name()] = true
	}

	for _, want := range []string{"version", "inspect", "sync"} {
		if !registered[want] {
			t.Errorf("NewRootCmd() does not register a %q subcommand", want)
		}
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	got := Run([]string{"no-such-command"}, &stdout, &stderr)

	if got != 1 {
		t.Errorf("Run() exit code = %d, want 1 (stderr: %s)", got, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("stderr = %q, want it to mention the unknown command", stderr.String())
	}
}

func TestRun_Help(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	got := Run([]string{"--help"}, &stdout, &stderr)

	if got != 0 {
		t.Errorf("Run() exit code = %d, want 0 (stderr: %s)", got, stderr.String())
	}
	if !strings.Contains(stdout.String(), "dashsync") {
		t.Errorf("stdout = %q, want usage text mentioning dashsync", stdout.String())
	}
}
