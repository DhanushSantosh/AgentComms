package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureRuntimeHiddenCreatesGitignore(t *testing.T) {
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
	// Idempotent: running it again must not duplicate entries.
	if err := instance.EnsureRuntimeHidden(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{"/.agents", "/.agent-comms/"} {
		if count := strings.Count(string(raw), rule); count != 1 {
			t.Fatalf("%s count=%d in .gitignore, want 1:\n%s", rule, count, raw)
		}
	}
}

func TestEnsureRuntimeHiddenAppendsToExistingGitignore(t *testing.T) {
	root := t.TempDir()
	command := exec.Command("git", "init")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s", output)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	instance := Open(root)
	if err := instance.EnsureRuntimeHidden(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "node_modules/") {
		t.Fatalf("pre-existing entry was lost:\n%s", raw)
	}
	for _, rule := range []string{"/.agents", "/.agent-comms/"} {
		if !strings.Contains(string(raw), rule) {
			t.Fatalf("missing %s:\n%s", rule, raw)
		}
	}
}

func TestEnsureRuntimeHiddenNoopsOutsideGitRepo(t *testing.T) {
	root := t.TempDir()
	instance := Open(root)
	if err := instance.EnsureRuntimeHidden(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("expected no .gitignore to be created outside a Git repo, stat err=%v", err)
	}
}
