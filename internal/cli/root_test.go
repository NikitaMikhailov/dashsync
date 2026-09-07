package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewRootCmd_RegistersVersionSubcommand(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "version" {
			found = true
			break
		}
	}
	if !found {
		t.Error("NewRootCmd() does not register a version subcommand")
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
