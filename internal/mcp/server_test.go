package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/DhanushSantosh/AgentComms/internal/testsupport"
)

const testServerVersion = "test-version"

// asActor builds a bare ActorResolution for tests that only care about the
// bound actor, not how it was resolved.
func asActor(actor string) identity.ActorResolution {
	return identity.ActorResolution{Actor: actor}
}

// grantOrchestrator drives the two-step apply-then-approve flow the
// ORCHESTRATOR role now requires (internal/protocol/transitions.go):
// approver applies a HUMAN-tier approval for this exact grant, separately
// approves it, then activates id as ORCHESTRATOR.
func grantOrchestrator(t *testing.T, instance *service.Service, approver, id string, scopes []string) {
	t.Helper()
	approvalID := id + "-orchestrator-approval"
	if _, e := instance.Execute(approver, "approval.request", approvalID, model.ApprovalRequested{
		Tier: "HUMAN", Action: protocol.OrchestratorGrantApprovalAction(id), Reason: "test fixture",
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := instance.Execute(approver, "approval.approve", approvalID, model.ApprovalResponse{}); e != nil {
		t.Fatal(e)
	}
	if _, e := instance.Execute(approver, "agent.activate", id,
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: scopes}); e != nil {
		t.Fatal(e)
	}
}

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

// TestProjectUpgradeStatusReportsProjectLifecycleErrorCode guards finding
// 8: a *projectlifecycle.Error (here CodeProjectTooNew, from a project
// whose recorded MinimumToolkit is newer than the running binary) must
// report its real stable code over MCP's error.data.code, the same way
// internal/app/app.go's CLI errorCode/exitCode already does, rather than
// rpcFail falling back to a generic classification.
func TestProjectUpgradeStatusReportsProjectLifecycleErrorCode(t *testing.T) {
	instance, root := testsupport.StartPersonalProject(t)
	configPath := filepath.Join(root, store.Runtime, "config.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	config["minimum_toolkit_version"] = "999.0.0"
	raw, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"project_upgrade_status","arguments":{}}}` + "\n"
	var output bytes.Buffer
	if err = Serve(instance, asActor("owner"), testServerVersion, strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"code":"PROJECT_TOO_NEW"`) {
		t.Fatalf("expected error.data.code=PROJECT_TOO_NEW, got: %s", output.String())
	}
}

func TestInitializeAndToolCatalog(t *testing.T) {
	s, _ := testsupport.StartPersonalProject(t)
	input := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n"
	var out bytes.Buffer
	if e := Serve(s, asActor("owner"), testServerVersion, strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), `"version":"`+testServerVersion+`"`) {
		t.Fatalf("initialize did not report the supplied binary version: %s", out.String())
	}
	for _, want := range []string{
		"agent-comms", `"identity"`, `"get_started"`, "task_create", "message_post", "invocation_request",
		"invocation_next", "invocation_listen", "invocation_claim",
		"runtime_register", "runtime_heartbeat", "verify", `"agent_revoke"`,
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
	resolution := identity.ActorResolution{Actor: "AXIOM", Source: identity.ActorSourceHostBinding, HostLabel: "claude"}
	if err := Serve(instance, resolution, testServerVersion, strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"actor":"AXIOM"`) || !strings.Contains(output.String(), `"source":"host_binding"`) {
		t.Fatalf("identity tool did not report the bound actor and resolution source: %s", output.String())
	}
}

// TestGetStartedToolReportsRegistrationState proves the "get_started" MCP
// tool — the fix for MCP-connected agents having no path to onboarding
// content at all — actually reflects live state, not a static blurb: it
// must flip from unregistered to registered+active as the same actor
// progresses through register/activate, the same way the CLI's
// agent-instructions command does.
func TestGetStartedToolReportsRegistrationState(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_started","arguments":{}}}` + "\n"
	resolution := identity.ActorResolution{Actor: "fresh-agent", Source: identity.ActorSourceProjectOwner}

	var before bytes.Buffer
	if err := Serve(instance, resolution, testServerVersion, strings.NewReader(input), &before); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before.String(), `"registered":false`) || !strings.Contains(before.String(), `"active":false`) {
		t.Fatalf("expected unregistered state before registration: %s", before.String())
	}
	if !strings.Contains(before.String(), `agent_register`) {
		t.Fatalf("expected get_started's guide to mention agent_register: %s", before.String())
	}

	if _, err := instance.Register("fresh-agent", "Fresh Agent", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Execute("owner", "agent.activate", "fresh-agent",
		model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}}); err != nil {
		t.Fatal(err)
	}

	var after bytes.Buffer
	registeredResolution := identity.ActorResolution{Actor: "fresh-agent", Source: identity.ActorSourceHostBinding, HostLabel: "claude"}
	if err := Serve(instance, registeredResolution, testServerVersion, strings.NewReader(input), &after); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after.String(), `"registered":true`) || !strings.Contains(after.String(), `"active":true`) {
		t.Fatalf("expected registered+active state after register+activate: %s", after.String())
	}
	if !strings.Contains(after.String(), `"role":"AGENT"`) {
		t.Fatalf("expected role AGENT in get_started response: %s", after.String())
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
	if err := Serve(instance, asActor("builder"), testServerVersion, strings.NewReader(input), &output); err != nil {
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
	if e := Serve(s, asActor("owner"), testServerVersion, strings.NewReader(input), &out); e != nil {
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
	if e := Serve(instance, asActor("fresh-agent"), testServerVersion, strings.NewReader(registerInput), &registerOut); e != nil {
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
	if e := Serve(instance, asActor("owner"), testServerVersion, strings.NewReader(activateInput), &activateOut); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(activateOut.String(), `"error"`) {
		t.Fatalf("agent_activate failed: %s", activateOut.String())
	}

	runtimeInput := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"runtime_register","arguments":{"id":"fresh-runtime","connector":"MCP","max_concurrent":1}}}` + "\n"
	var runtimeOut bytes.Buffer
	if e := Serve(instance, asActor("fresh-agent"), testServerVersion, strings.NewReader(runtimeInput), &runtimeOut); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(runtimeOut.String(), `"error"`) {
		t.Fatalf("runtime_register with the newly-registered agent's own signature failed: %s", runtimeOut.String())
	}
}

// TestAgentRegisterToolRejectsSpoofedID guards a real vulnerability caught
// by security review: the first implementation never checked that the
// requested id matched the connection's bound actor at all — an MCP
// connection scoped to one actor could register (or squat) an unrelated
// identity entirely. Registering on behalf of a different id is now a real,
// named capability (an active orchestrator or human principal may sponsor
// a new registration), but an unregistered, unprivileged actor like
// "codex-runner" here must still be rejected — this must not regress into
// "any actor can register any id" again.
func TestAgentRegisterToolRejectsSpoofedID(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)

	spoofInput := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_register","arguments":{"id":"someone-else"}}}` + "\n"
	var spoofOut bytes.Buffer
	if e := Serve(instance, asActor("codex-runner"), testServerVersion, strings.NewReader(spoofInput), &spoofOut); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(spoofOut.String(), `"error"`) {
		t.Fatalf("expected agent_register to reject id != actor, got: %s", spoofOut.String())
	}
	if !strings.Contains(spoofOut.String(), `"data":{"code":"AUTHORIZATION"}`) {
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
	if e := Serve(instance, asActor("owner"), testServerVersion, strings.NewReader(input), &out); e != nil {
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

// TestAgentRegisterToolPermitsActiveOrchestratorSponsorship proves
// CanSponsorRegistration's general rule, not just the owner special case:
// any active ORCHESTRATOR-role principal (human or agent) may register a
// new agent on its behalf, exactly like the owner can.
func TestAgentRegisterToolPermitsActiveOrchestratorSponsorship(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)
	if _, e := instance.Register("lead", "Lead", model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}
	grantOrchestrator(t, instance, "owner", "lead", []string{"src"})

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_register","arguments":{"id":"sponsored-agent"}}}` + "\n"
	var out bytes.Buffer
	if e := Serve(instance, asActor("lead"), testServerVersion, strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), `"error"`) {
		t.Fatalf("active orchestrator sponsorship should have succeeded: %s", out.String())
	}
	state, e := instance.State()
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := state.Agents["sponsored-agent"]; !ok {
		t.Fatalf("sponsored-agent was not registered: %+v", state.Agents)
	}
}

// TestAgentRegisterToolRejectsNonOrchestratorAgentSponsorship guards the
// other half: an active but plain AGENT-role, AGENT-principal-type actor —
// neither an orchestrator nor human — must not be able to register on
// behalf of a different id, even though it's a real, registered, active
// principal (unlike the spoofing test's unregistered "codex-runner").
func TestAgentRegisterToolRejectsNonOrchestratorAgentSponsorship(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)
	if _, e := instance.Register("reviewer", "Reviewer", model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}
	if _, e := instance.Execute("owner", "agent.activate", "reviewer",
		model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}}); e != nil {
		t.Fatal(e)
	}

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_register","arguments":{"id":"someone-else"}}}` + "\n"
	var out bytes.Buffer
	if e := Serve(instance, asActor("reviewer"), testServerVersion, strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), `"data":{"code":"AUTHORIZATION"}`) {
		t.Fatalf("expected a plain agent's sponsorship attempt to be rejected as AUTHORIZATION, got: %s", out.String())
	}
	state, e := instance.State()
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := state.Agents["someone-else"]; ok {
		t.Fatal("non-orchestrator sponsorship must not have created the principal")
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
	if e := Serve(instance, asActor("fresh-agent"), testServerVersion, strings.NewReader(input), &out); e != nil {
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
	if e := Serve(instance, asActor("bystander"), testServerVersion, strings.NewReader(activateInput), &unauthorizedOut); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(unauthorizedOut.String(), `"error"`) {
		t.Fatalf("expected a non-owner actor's agent_activate to be rejected, got: %s", unauthorizedOut.String())
	}

	var ownerOut bytes.Buffer
	if e := Serve(instance, asActor("owner"), testServerVersion, strings.NewReader(activateInput), &ownerOut); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(ownerOut.String(), `"error"`) {
		t.Fatalf("expected the owner's agent_activate to succeed, got: %s", ownerOut.String())
	}
}

// TestAgentActivateToolRequiresHumanToGrantOrchestratorRole proves the
// orchestrator-escalation hard check (internal/protocol/transitions.go) is
// enforced over MCP, not just at the Service layer: an AGENT-principal
// orchestrator must not be able to mint a further orchestrator through the
// agent_activate tool, even though it already passes the ordinary
// owner-or-orchestrator elevation gate.
func TestAgentActivateToolRequiresHumanToGrantOrchestratorRole(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)
	if _, e := instance.Register("agent-lead", "Agent Lead", model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}
	grantOrchestrator(t, instance, "owner", "agent-lead", []string{"src"})
	if _, e := instance.Register("candidate", "Candidate", model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_activate","arguments":{"id":"candidate","role":"ORCHESTRATOR"}}}` + "\n"
	var agentOut bytes.Buffer
	if e := Serve(instance, asActor("agent-lead"), testServerVersion, strings.NewReader(input), &agentOut); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(agentOut.String(), `"data":{"code":"AUTHORIZATION"}`) {
		t.Fatalf("expected an agent-principal orchestrator's grant to be rejected as AUTHORIZATION, got: %s", agentOut.String())
	}
	state, e := instance.State()
	if e != nil {
		t.Fatal(e)
	}
	if state.Agents["candidate"].Role == model.RoleOrchestrator {
		t.Fatal("candidate must not have been granted the orchestrator role")
	}

	// The human-principal check alone is not enough either: the owner's own
	// grant must also fail without a prior, separately-approved, HUMAN-tier
	// approval on record for this exact id.
	var unapprovedOut bytes.Buffer
	if e := Serve(instance, asActor("owner"), testServerVersion, strings.NewReader(input), &unapprovedOut); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(unapprovedOut.String(), `"error"`) {
		t.Fatalf("expected the owner's grant to be rejected without a prior approval, got: %s", unapprovedOut.String())
	}
	if _, e := instance.Execute("owner", "approval.request", "candidate-orchestrator-approval", model.ApprovalRequested{
		Tier: "HUMAN", Action: protocol.OrchestratorGrantApprovalAction("candidate"), Reason: "test",
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := instance.Execute("owner", "approval.approve", "candidate-orchestrator-approval", model.ApprovalResponse{}); e != nil {
		t.Fatal(e)
	}

	var ownerOut bytes.Buffer
	if e := Serve(instance, asActor("owner"), testServerVersion, strings.NewReader(input), &ownerOut); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(ownerOut.String(), `"error"`) {
		t.Fatalf("expected the human owner's orchestrator grant to succeed once approved, got: %s", ownerOut.String())
	}
}

// TestMCPElevatedKeyTransitionsFailClosed proves the exact threat model
// internal/app/passphrase.go's own doc comment names: an MCP-connected
// agent has no interactive terminal to answer a passphrase prompt with, so
// once an elevated key is registered for an actor, agent_activate(ORCHESTRATOR)
// and agent_revoke of an orchestrator must fail closed over MCP, not hang or
// silently fall back to the primary key. This test never wires
// instance.PassphrasePrompt at all, mirroring the realistic MCP default --
// internal/app wires mcp specifically to a prompt that always refuses
// (nonInteractivePassphrasePrompt), and a bare *service.Service like this
// one has a nil prompt by construction either way. (approval_approve isn't
// tested here because it isn't an MCP tool at all -- server.go's tools()
// never registers it.)
func TestMCPElevatedKeyTransitionsFailClosed(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)
	if _, e := instance.Register("candidate", "Candidate", model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}
	if _, e := instance.Execute("owner", "agent.activate", "candidate", model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}}); e != nil {
		t.Fatal(e)
	}
	if _, e := instance.ElevateKey("owner", "a strong passphrase"); e != nil {
		t.Fatal(e)
	}
	if _, e := instance.Execute("owner", "approval.request", "candidate-orchestrator-approval", model.ApprovalRequested{
		Tier: "HUMAN", Action: protocol.OrchestratorGrantApprovalAction("candidate"), Reason: "test",
	}); e != nil {
		t.Fatal(e)
	}
	// approval.approve isn't exposed over MCP, so approve this directly
	// through the service to reach the activate-over-MCP scenario below --
	// this direct call also goes through instance.PassphrasePrompt (nil),
	// so it doubles as proof the approve step itself fails closed too.
	if _, e := instance.Execute("owner", "approval.approve", "candidate-orchestrator-approval", model.ApprovalResponse{}); e == nil {
		t.Fatal("expected approval.approve to fail closed with no PassphrasePrompt configured, once an elevated key is registered")
	}

	activateInput := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_activate","arguments":{"id":"candidate","role":"ORCHESTRATOR"}}}` + "\n"
	var activateOut bytes.Buffer
	if e := Serve(instance, asActor("owner"), testServerVersion, strings.NewReader(activateInput), &activateOut); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(activateOut.String(), `"error"`) {
		t.Fatalf("expected agent_activate(ORCHESTRATOR) over MCP to fail closed once an elevated key is registered, got: %s", activateOut.String())
	}
	state, e := instance.State()
	if e != nil {
		t.Fatal(e)
	}
	if state.Agents["candidate"].Role == model.RoleOrchestrator {
		t.Fatal("candidate must not have been granted the orchestrator role over MCP")
	}

	// Grant it directly through the service using an explicit, correctly
	// answering prompt (bypassing MCP), so the revoke-over-MCP path below
	// has an actual orchestrator to target.
	instance.PassphrasePrompt = func(string) (string, error) { return "a strong passphrase", nil }
	if _, e := instance.Execute("owner", "approval.approve", "candidate-orchestrator-approval", model.ApprovalResponse{}); e != nil {
		t.Fatal(e)
	}
	if _, e := instance.Execute("owner", "agent.activate", "candidate", model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); e != nil {
		t.Fatal(e)
	}
	instance.PassphrasePrompt = nil

	revokeInput := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_revoke","arguments":{"id":"candidate"}}}` + "\n"
	var revokeOut bytes.Buffer
	if e := Serve(instance, asActor("owner"), testServerVersion, strings.NewReader(revokeInput), &revokeOut); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(revokeOut.String(), `"error"`) {
		t.Fatalf("expected agent_revoke of an orchestrator over MCP to fail closed once an elevated key is registered, got: %s", revokeOut.String())
	}
	state, e = instance.State()
	if e != nil {
		t.Fatal(e)
	}
	if state.Agents["candidate"].Status == "REVOKED" {
		t.Fatal("candidate must not have been revoked over MCP")
	}
}

// TestAgentRevokeToolRejectsAgentOrchestratorRevokingAnotherOrchestrator is
// the revoke-side sibling of TestAgentActivateToolRequiresHumanToGrantOrchestratorRole:
// an AGENT-principal orchestrator must not be able to unilaterally revoke a
// different orchestrator over MCP either.
func TestAgentRevokeToolRejectsAgentOrchestratorRevokingAnotherOrchestrator(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)
	if _, e := instance.Register("agent-lead", "Agent Lead", model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}
	grantOrchestrator(t, instance, "owner", "agent-lead", []string{"src"})
	if _, e := instance.Register("other-orchestrator", "Other Orchestrator", model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}
	grantOrchestrator(t, instance, "owner", "other-orchestrator", []string{"src"})

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_revoke","arguments":{"id":"other-orchestrator"}}}` + "\n"
	var agentOut bytes.Buffer
	if e := Serve(instance, asActor("agent-lead"), testServerVersion, strings.NewReader(input), &agentOut); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(agentOut.String(), `"data":{"code":"AUTHORIZATION"}`) {
		t.Fatalf("expected an agent-principal orchestrator's revoke to be rejected as AUTHORIZATION, got: %s", agentOut.String())
	}

	var ownerOut bytes.Buffer
	if e := Serve(instance, asActor("owner"), testServerVersion, strings.NewReader(input), &ownerOut); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(ownerOut.String(), `"error"`) {
		t.Fatalf("expected the human owner's revoke to succeed, got: %s", ownerOut.String())
	}
	state, e := instance.State()
	if e != nil {
		t.Fatal(e)
	}
	if state.Agents["other-orchestrator"].Status != "REVOKED" {
		t.Fatalf("other-orchestrator was not revoked: %+v", state.Agents["other-orchestrator"])
	}
}

// TestAgentRevokeToolRejectsOwnerTarget guards against ever bricking a
// project down to zero owners via the MCP tool.
func TestAgentRevokeToolRejectsOwnerTarget(t *testing.T) {
	instance, _ := testsupport.StartPersonalProject(t)
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_revoke","arguments":{"id":"owner"}}}` + "\n"
	var out bytes.Buffer
	if e := Serve(instance, asActor("owner"), testServerVersion, strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), `"error"`) {
		t.Fatalf("expected revoking the owner to be rejected, got: %s", out.String())
	}
}
