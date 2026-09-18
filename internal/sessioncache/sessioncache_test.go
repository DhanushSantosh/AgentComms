package sessioncache

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPathNeverPointsInsideRoot(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	path, err := Path(root, "claude-serve")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(path, root) {
		t.Fatalf("Path returned a path inside root: %s (root=%s)", path, root)
	}
	if filepath.Ext(path) != ".json" {
		t.Fatalf("expected a .json path, got %s", path)
	}
}

func TestPathIsStableForTheSameRootAndKind(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	first, err := Path(root, "codex-serve")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Path(root, "codex-serve")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("Path is not stable: %q != %q", first, second)
	}
}

func TestPathDiffersByKindForTheSameRoot(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	claudePath, err := Path(root, "claude-serve")
	if err != nil {
		t.Fatal(err)
	}
	codexPath, err := Path(root, "codex-serve")
	if err != nil {
		t.Fatal(err)
	}
	if claudePath == codexPath {
		t.Fatal("expected different kinds for the same root to produce different paths")
	}
}

func TestPathDiffersByRootForTheSameKind(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	first, err := Path(t.TempDir(), "runtime-sessions")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Path(t.TempDir(), "runtime-sessions")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("expected different roots to produce different paths")
	}
}
