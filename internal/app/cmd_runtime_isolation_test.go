package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeWorkerCodexACPRejectsUnsupportedIsolation(t *testing.T) {
	project := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	cleanupProjectDaemon(t, project)
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"init", "--project", project, "--non-interactive", "--owner", "owner", "--json"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	err := Run([]string{
		"runtime", "worker", "--project", project, "--actor", "owner",
		"--adapter", "codex-acp", "--id", "unregistered-runtime",
		"--codex-ignore-user-config", "--once", "--non-interactive",
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "codex-acp") || !strings.Contains(err.Error(), "--adapter codex") {
		t.Fatalf("public CLI must reject unsupported isolation before runtime execution: %v", err)
	}
}
