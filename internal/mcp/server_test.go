package mcp

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
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
	for _, want := range []string{
		"agent-comms", "task_create", "message_post", "invocation_request",
		"invocation_next", "invocation_listen", "invocation_claim",
		"runtime_register", "runtime_heartbeat", "verify",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestInvocationToolsReturnAndClaimWork(t *testing.T) {
	d := t.TempDir()
	command := exec.Command("git", "init")
	command.Dir = d
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatal(string(output))
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(d, "user"))
	instance := service.New(d)
	instance.Store.SetCredentialStore(identity.NewMemoryStore())
	if err := instance.Store.Init("owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Register("builder", "Builder", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Execute("owner", "agent.activate", "builder",
		model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Execute("builder", "runtime.register", "runtime-builder",
		model.RuntimeRegistered{AgentID: "builder", Connector: "MCP", MaxConcurrent: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Execute("owner", "invocation.request", "inv-mcp",
		model.InvocationRequested{Target: "builder", Instruction: "Exercise MCP"}); err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"invocation_listen","arguments":{"runtime_id":"runtime-builder","wait_seconds":1}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := Serve(instance, "builder", strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"found":true`) ||
		!strings.Contains(output.String(), `"claimed":true`) ||
		!strings.Contains(output.String(), `"type":"invocation.claim"`) {
		t.Fatalf("unexpected invocation tool output: %s", output.String())
	}
}
