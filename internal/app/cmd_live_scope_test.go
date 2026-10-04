package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/brokeridentity"
)

func TestLiveAttachExplicitProjectRouting(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			key, _ := brokeridentity.RuntimeKey("project-a", "runtime-a")
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				http.Error(w, "absent", http.StatusNotFound)
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			err := Run([]string{"live", "attach", "--provider", provider, "--runtime", "runtime-a", "--project-id", "project-a", "--server", server.URL}, &out, &stderr)
			if err == nil {
				t.Fatal("missing scoped runtime accepted")
			}
			if len(paths) != 1 || paths[0] != "/runtimes/"+key+"/events" {
				t.Fatalf("wrong route or legacy fallback: %v", paths)
			}
		})
	}
}

func TestLiveAttachScopeSelection(t *testing.T) {
	c := &cli{project: filepath.Join(t.TempDir(), "missing")}
	if _, err := c.liveAttachProjectID("", false, false, "runtime"); err == nil {
		t.Fatal("explicit missing checkout silently became unscoped")
	}
	if id, err := c.liveAttachProjectID("", false, true, "runtime"); err != nil || id != "" {
		t.Fatalf("legacy override failed: %q %v", id, err)
	}
	if _, err := c.liveAttachProjectID("", true, false, "runtime"); err == nil {
		t.Fatal("empty explicit project ID accepted")
	}
	if _, err := c.liveAttachProjectID("project", true, true, "runtime"); err == nil {
		t.Fatal("conflicting scope accepted")
	}
}

func TestLiveAttachLocalProjectRouting(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-comms"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".agent-comms", "config.json")
	if err := os.WriteFile(path, []byte(`{"runtime_mode":"personal","project_id":"stored-project"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &cli{project: root}
	if id, err := c.liveAttachProjectID("", false, false, "runtime"); err != nil || id != "stored-project" {
		t.Fatalf("local identity: %q %v", id, err)
	}
	t.Chdir(root)
	c.project = ""
	if id, err := c.liveAttachProjectID("", false, false, "runtime"); err != nil || id != "stored-project" {
		t.Fatalf("cwd identity: %q %v", id, err)
	}
	if err := os.WriteFile(path, []byte(`{"runtime_mode":"personal","unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.liveAttachProjectID("", false, false, "runtime"); err == nil {
		t.Fatal("malformed local config silently became unscoped")
	}
}
