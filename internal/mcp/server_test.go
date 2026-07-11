package mcp

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

func TestInitializeAndToolCatalog(t *testing.T) {
	d := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = d
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(string(out))
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(d, "user"))
	s := service.New(d)
	s.Store.SetCredentialStore(identity.NewMemoryStore())
	if e := s.Store.Init("owner"); e != nil {
		t.Fatal(e)
	}
	input := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n"
	var out bytes.Buffer
	if e := Serve(s, "owner", strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"agent-comms", "task_create", "message_post", "verify"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %s", want)
		}
	}
}
