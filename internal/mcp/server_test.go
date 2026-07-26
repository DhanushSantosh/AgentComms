package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/testsupport"
)

const testServerVersion = "test-version"

// TestToolSchemasNeverEmitNullRequired guards a real, live-discovered bug:
// a tool with zero required arguments (status, history, invocation_next,
// verify) used to marshal "required":null (Go's zero value for a variadic
// called with no args), which is invalid per JSON Schema and caused Claude
// Code's real MCP client to fetch the tool list successfully but silently
// reject the whole thing ("tools fetch failed", no further detail).
func TestToolSchemasNeverEmitNullRequired(t *testing.T) {
	for _, def := range tools() {
		schema, ok := def["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("tool %v has no inputSchema", def["name"])
		}
		b, err := json.Marshal(schema)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `"required":null`) {
			t.Fatalf("tool %v marshals required as null: %s", def["name"], b)
		}
	}
}

func TestInitializeAndToolCatalog(t *testing.T) {
	s, _ := testsupport.StartPersonalProject(t)
	input := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n"
	var out bytes.Buffer
	if e := Serve(s, "owner", testServerVersion, strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), `"version":"`+testServerVersion+`"`) {
		t.Fatalf("initialize did not report the supplied binary version: %s", out.String())
	}
	for _, want := range []string{
		"agent-comms", `"identity"`, "task_create", "message_post", "invocation_request",
		"invocation_next", "invocation_listen", "invocation_claim",
		"runtime_register", "runtime_heartbeat", "verify",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestIdentityToolReportsConnectionActor(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"identity","arguments":{}}}` + "\n"
	var output bytes.Buffer
	if err := Serve(instance, "AXIOM", testServerVersion, strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"actor":"AXIOM"`) {
		t.Fatalf("identity tool did not report the bound actor: %s", output.String())
	}
}

func TestInvocationToolsReturnAndClaimWork(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)
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
	if err := Serve(instance, "builder", testServerVersion, strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"found":true`) ||
		!strings.Contains(output.String(), `"claimed":true`) ||
		!strings.Contains(output.String(), `"type":"invocation.claim"`) {
		t.Fatalf("unexpected invocation tool output: %s", output.String())
	}
}

// TestNotificationsReceiveNoResponse guards a real JSON-RPC 2.0 requirement:
// notifications (no "id" — by MCP convention, any "notifications/*" method)
// never get a response, even though this server previously sent one for
// notifications/initialized. A strict client's response correlation could
// break if a reply arrives for a message it never expects one for.
func TestNotificationsReceiveNoResponse(t *testing.T) {
	s, _ := testsupport.StartPersonalProject(t)
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
	var out bytes.Buffer
	if e := Serve(s, "owner", testServerVersion, strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected exactly one response line (none for the notification), got %d: %q", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"id":1`) {
		t.Fatalf("expected the one response to be initialize's, got: %s", lines[0])
	}
}

// TestAgentRegisterToolCreatesCredential proves the bootstrap gap is
// actually closed, not just that the tool returns success: a fresh agent
// self-registers over its own MCP connection, gets activated (a separate
// connection bound as the owner, since agent.activate is elevated), and
// then its own follow-up call requiring a real signature succeeds.
func TestAgentRegisterToolCreatesCredential(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)

	registerInput := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_register","arguments":{"id":"fresh-agent"}}}` + "\n"
	var registerOut bytes.Buffer
	if e := Serve(instance, "fresh-agent", testServerVersion, strings.NewReader(registerInput), &registerOut); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(registerOut.String(), `"error"`) {
		t.Fatalf("agent_register failed: %s", registerOut.String())
	}
	state, e := instance.State()
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := state.Agents["fresh-agent"]; !ok {
		t.Fatalf("fresh-agent was not registered: %+v", state.Agents)
	}

	activateInput := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_activate","arguments":{"id":"fresh-agent","role":"AGENT","scopes":["src"]}}}` + "\n"
	var activateOut bytes.Buffer
	if e := Serve(instance, "owner", testServerVersion, strings.NewReader(activateInput), &activateOut); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(activateOut.String(), `"error"`) {
		t.Fatalf("agent_activate failed: %s", activateOut.String())
	}

	runtimeInput := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"runtime_register","arguments":{"id":"fresh-runtime","connector":"MCP","max_concurrent":1}}}` + "\n"
	var runtimeOut bytes.Buffer
	if e := Serve(instance, "fresh-agent", testServerVersion, strings.NewReader(runtimeInput), &runtimeOut); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(runtimeOut.String(), `"error"`) {
		t.Fatalf("runtime_register with the newly-registered agent's own signature failed: %s", runtimeOut.String())
	}
}

// TestAgentRegisterToolRejectsSpoofedID guards a real vulnerability caught
// by security review: agent_register's own docstring promises
// self-registration only, but the first implementation never actually
// checked that the requested id matched the connection's bound actor — an
// MCP connection scoped to one actor could register (or squat) an
// unrelated identity entirely, breaking the per-actor scoping MCP
// connections are supposed to guarantee.
func TestAgentRegisterToolRejectsSpoofedID(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)

	spoofInput := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_register","arguments":{"id":"someone-else"}}}` + "\n"
	var spoofOut bytes.Buffer
	if e := Serve(instance, "codex-runner", testServerVersion, strings.NewReader(spoofInput), &spoofOut); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(spoofOut.String(), `"error"`) {
		t.Fatalf("expected agent_register to reject id != actor, got: %s", spoofOut.String())
	}
	if !strings.Contains(spoofOut.String(), `"data":{"code":"VALIDATION"}`) {
		t.Fatalf("expected stable MCP error classification, got: %s", spoofOut.String())
	}
	state, e := instance.State()
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := state.Agents["someone-else"]; ok {
		t.Fatal("spoofed registration must not have been created")
	}
}

// TestAgentRegisterToolPermitsOwnerFallbackBootstrap covers the new
// bootstrap case: a brand-new project has no stored profile for any
// (project, host) pair yet, so a connection resolves to the project owner
// by fallback. That owner-fallback connection must be able to register a
// freshly-chosen, meaningful ID (e.g. "AXIOM") rather than being stuck
// self-registering literally as "owner" — the owner already has authority
// to create new principals, so this is bootstrapping a new identity, not
// impersonating an existing one.
func TestAgentRegisterToolPermitsOwnerFallbackBootstrap(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_register","arguments":{"id":"AXIOM"}}}` + "\n"
	var out bytes.Buffer
	if e := Serve(instance, "owner", testServerVersion, strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), `"error"`) {
		t.Fatalf("owner-fallback bootstrap registration should have succeeded: %s", out.String())
	}
	state, e := instance.State()
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := state.Agents["AXIOM"]; !ok {
		t.Fatalf("AXIOM was not registered: %+v", state.Agents)
	}
}

// TestAgentRegisterToolRejectsInvalidPrincipalType guards the second half
// of the same finding: principal_type was cast straight from an
// unvalidated string to model.PrincipalType, even though the tool's own
// JSON Schema declares it as an enum of exactly HUMAN/AGENT — a client
// that ignores its own schema could otherwise smuggle an arbitrary value
// through.
func TestAgentRegisterToolRejectsInvalidPrincipalType(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_register","arguments":{"id":"fresh-agent","principal_type":"OWNER"}}}` + "\n"
	var out bytes.Buffer
	if e := Serve(instance, "fresh-agent", testServerVersion, strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), `"error"`) {
		t.Fatalf("expected agent_register to reject an invalid principal_type, got: %s", out.String())
	}
}

// TestAgentActivateToolRequiresElevation confirms agent_activate is exactly
// as gated over MCP as agent.activate already is everywhere else — this
// tool must not loosen that rule.
func TestAgentActivateToolRequiresElevation(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)
	if _, e := instance.Register("bystander", "Bystander", model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}
	if _, e := instance.Execute("owner", "agent.activate", "bystander",
		model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}}); e != nil {
		t.Fatal(e)
	}
	if _, e := instance.Register("fresh-agent", "Fresh Agent", model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}

	activateInput := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_activate","arguments":{"id":"fresh-agent","role":"AGENT"}}}` + "\n"
	var unauthorizedOut bytes.Buffer
	if e := Serve(instance, "bystander", testServerVersion, strings.NewReader(activateInput), &unauthorizedOut); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(unauthorizedOut.String(), `"error"`) {
		t.Fatalf("expected a non-owner actor's agent_activate to be rejected, got: %s", unauthorizedOut.String())
	}

	var ownerOut bytes.Buffer
	if e := Serve(instance, "owner", testServerVersion, strings.NewReader(activateInput), &ownerOut); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(ownerOut.String(), `"error"`) {
		t.Fatalf("expected the owner's agent_activate to succeed, got: %s", ownerOut.String())
	}
}
