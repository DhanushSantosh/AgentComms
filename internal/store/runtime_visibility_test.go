package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureRuntimeHiddenUsesLocalGitExclude(t *testing.T) {
	root := t.TempDir()
	command := exec.Command("git", "init")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s", output)
	}
	instance := Open(root)
	if err := instance.EnsureRuntimeHidden(); err != nil {
		t.Fatal(err)
	}
	if err := instance.EnsureRuntimeHidden(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(raw), "/.agent-comms/"); count != 1 {
		t.Fatalf("runtime exclude count=%d, want 1", count)
	}
}
