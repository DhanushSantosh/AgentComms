package claudepath

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionPathUsesFilesystemSafeProjectSlug(t *testing.T) {
	projectDirectory := filepath.Join(t.TempDir(), "project")
	claudeHome := filepath.Join(t.TempDir(), ".claude")
	path, err := SessionPath(claudeHome, projectDirectory, "session-one")
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Join(claudeHome, "projects"), path)
	if err != nil {
		t.Fatal(err)
	}
	components := strings.Split(filepath.ToSlash(relative), "/")
	if len(components) != 2 || components[1] != "session-one.jsonl" {
		t.Fatalf("unexpected Claude session path %q", path)
	}
	if strings.ContainsAny(components[0], `/\:`) {
		t.Fatalf("project slug contains a filesystem separator or volume delimiter: %q", components[0])
	}
}
