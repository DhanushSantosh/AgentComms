package app

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestProjectUpgradeStatusNoLongerExists(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	project := t.TempDir()
	var stdout, stderr bytes.Buffer
	run := func(args ...string) error {
		stdout.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json", "--quiet")
		return Run(args, &stdout, &stderr)
	}
	if err := run("init", "--non-interactive", "--owner", "owner", "--mode", "personal"); err != nil {
		t.Fatalf("init: %v\n%s", err, stderr.String())
	}
	if err := run("project", "upgrade", "status"); err == nil {
		t.Fatal("expected `project upgrade status` to no longer exist")
	}
	if err := run("project", "upgrade", "plan"); err != nil {
		t.Fatalf("project upgrade plan: %v\n%s", err, stderr.String())
	}
}
