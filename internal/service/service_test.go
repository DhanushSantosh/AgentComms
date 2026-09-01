package service_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/testsupport"
)

func setup(t *testing.T) *service.Service {
	t.Helper()
	s, _ := testsupport.StartPersonalProject(t)
	return s
}

func setupWithLocalConnector(t *testing.T) *service.Service {
	t.Helper()
	configDirectory := t.TempDir()
	outcomePath := filepath.Join(configDirectory, "connector-outcome")
	if err := os.WriteFile(outcomePath, []byte("success"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDirectory, "connectors.json")
	raw, err := json.Marshal(map[string]any{
		"connectors": map[string]any{
			"test-local-process": map[string]any{
				"type": "LOCAL_PROCESS", "executable": os.Args[0], "timeout": "5s",
				"arguments": []string{"-test.run=TestServiceConnectorHelperProcess", "--"},
				"environment": map[string]string{
					"SERVICE_CONNECTOR_HELPER":  "1",
					"SERVICE_CONNECTOR_OUTCOME": outcomePath,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_COMMS_CONNECTOR_CONFIG", configPath)
	t.Setenv("AGENT_COMMS_TEST_CONNECTOR_OUTCOME", outcomePath)
	return setup(t)
}

func TestServiceConnectorHelperProcess(t *testing.T) {
	if os.Getenv("SERVICE_CONNECTOR_HELPER") != "1" {
		return
	}
	outcome, err := os.ReadFile(os.Getenv("SERVICE_CONNECTOR_OUTCOME"))
	if err != nil {
		os.Exit(2)
	}
	if strings.TrimSpace(string(outcome)) == "failure" {
		os.Exit(1)
	}
}
func must(t *testing.T, s *service.Service, a, k, id string, p any) {
	t.Helper()
	if _, e := s.Execute(a, k, id, p); e != nil {
		t.Fatalf("%s: %v", k, e)
	}
}
func activate(t *testing.T, s *service.Service, id string, pt model.PrincipalType) {
	t.Helper()
	if _, e := s.Register(id, id, pt); e != nil {
		t.Fatal(e)
	}
	must(t, s, "owner", "agent.activate", id, model.AgentActivated{Role: model.Role("MEMBER"), Scopes: []string{"src"}})
}

func registerOnlineWorker(t *testing.T, s *service.Service, agentID, runtimeID string, maxConcurrent int) {
	t.Helper()
	must(t, s, agentID, "runtime.register", runtimeID, model.RuntimeRegistered{
		AgentID: agentID, Kind: model.RuntimeKindWorker, Connector: "MCP",
		MaxConcurrent: maxConcurrent,
	})
	must(t, s, agentID, "runtime.heartbeat", runtimeID, model.RuntimeHeartbeat{Health: "HEALTHY"})
}

func registerOnlineDeliverableWorker(t *testing.T, s *service.Service, agentID, runtimeID string) {
	t.Helper()
	must(t, s, agentID, "runtime.register", runtimeID, model.RuntimeRegistered{
		AgentID: agentID, Kind: model.RuntimeKindWorker, Connector: "LOCAL_PROCESS",
		ConfigReference: "test-local-process", MaxConcurrent: 1,
	})
	must(t, s, agentID, "runtime.heartbeat", runtimeID, model.RuntimeHeartbeat{Health: "HEALTHY"})
}

// grantOrchestrator drives the two-step apply-then-approve flow the
// ORCHESTRATOR role now requires: approver "applies" a HUMAN-tier approval
// for this exact grant, then separately approves it, before finally
// activating id as ORCHESTRATOR. approver must already be elevated
// (owner/orchestrator) and a HUMAN principal for the approve step to work.
func grantOrchestrator(t *testing.T, s *service.Service, approver, id string, scopes []string) {
	t.Helper()
	approvalID := protocol.OrchestratorGrantApprovalID(id)
	must(t, s, approver, "approval.request", approvalID, model.ApprovalRequested{
		Tier: "HUMAN", Action: protocol.OrchestratorGrantApprovalAction(id), Reason: "test fixture",
	})
	must(t, s, approver, "approval.approve", approvalID, model.ApprovalResponse{})
	must(t, s, approver, "agent.activate", id, model.AgentActivated{Role: model.RoleOrchestrator, Scopes: scopes})
}
func TestIdentityTaskOfferLeaseAndHandoff(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	activate(t, s, "beta", model.PrincipalAgent)
	must(t, s, "owner", "task.create", "task-1", model.TaskCreated{Title: "Build", Repository: "local", Branch: "feature", Resources: []string{"src/api"}})
	must(t, s, "owner", "task.offer", "task-1", model.TaskOffered{To: "alpha", ExpiresAt: time.Now().Add(time.Hour)})
	must(t, s, "alpha", "task.claim", "task-1", model.TaskClaimed{})
	must(t, s, "alpha", "task.start", "task-1", model.TaskStatus{})
	must(t, s, "alpha", "task.handoff", "task-1", model.TaskHandoff{To: "beta", Summary: "ready"})
	st, _ := s.State()
	if st.Tasks["task-1"].Owner != "alpha" {
		t.Fatal("handoff changed ownership before acceptance")
	}
	must(t, s, "beta", "task.handoff.accept", "task-1", model.TaskStatus{})
	st, _ = s.State()
	if st.Tasks["task-1"].Owner != "beta" {
		t.Fatal("handoff not accepted")
	}
}
func TestOverlappingProtectedLease(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	activate(t, s, "beta", model.PrincipalAgent)
	must(t, s, "owner", "task.create", "one", model.TaskCreated{Title: "One", Repository: "local", Branch: "a", Resources: []string{"src"}})
	must(t, s, "owner", "task.create", "two", model.TaskCreated{Title: "Two", Repository: "local", Branch: "b", Resources: []string{"src/file.go"}})
	must(t, s, "alpha", "task.claim", "one", model.TaskClaimed{})
	if _, e := s.Execute("beta", "task.claim", "two", model.TaskClaimed{}); e == nil {
		t.Fatal("overlap was allowed")
	}
}
func TestTypedMessageObligations(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	must(t, s, "owner", "message.post", "m1", model.MessagePosted{Kind: "ACTION", To: []string{"alpha"}, Subject: "Run checks"})
	must(t, s, "alpha", "message.ack", "m1", model.MessageResponse{})
	st, _ := s.State()
	if st.Messages["m1"].Recipients[0].Status != "ACCEPTED" {
		t.Fatal("action was not accepted")
	}
	must(t, s, "alpha", "message.complete", "m1", model.MessageResponse{})
	st, _ = s.State()
	if st.Messages["m1"].Status != "SATISFIED" {
		t.Fatal("action not satisfied")
	}
}
func TestHumanApprovalPolicy(t *testing.T) {
	s := setup(t)
	activate(t, s, "bot", model.PrincipalAgent)
	must(t, s, "owner", "approval.request", "a1", model.ApprovalRequested{Tier: "HUMAN", Action: "delete external data", Reason: "cleanup"})
	if _, e := s.Execute("bot", "approval.approve", "a1", model.ApprovalResponse{}); e == nil {
		t.Fatal("agent approved human tier")
	}
	must(t, s, "owner", "approval.approve", "a1", model.ApprovalResponse{})
}
func TestConcurrentWritersAndIntegrity(t *testing.T) {
	s := setup(t)
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := s.Execute("owner", "decision.create", string(rune('a'+i)), model.DecisionPayload{Title: "Decision", Statement: "Synthetic"})
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if e := s.Verify(0, 0); e != nil {
		t.Fatal(e)
	}
}

func TestConcurrentClaimsRevalidateInsideTransaction(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	activate(t, s, "beta", model.PrincipalAgent)
	must(t, s, "owner", "task.create", "exclusive", model.TaskCreated{
		Title: "Exclusive work", Repository: "local", Branch: "feature",
		Resources: []string{"src/exclusive"},
	})

	start := make(chan struct{})
	results := make(chan error, 2)
	var writers sync.WaitGroup
	for _, actor := range []string{"alpha", "beta"} {
		writers.Add(1)
		go func(actor string) {
			defer writers.Done()
			<-start
			_, err := s.Execute(actor, "task.claim", "exclusive", model.TaskClaimed{})
			results <- err
		}(actor)
	}
	close(start)
	writers.Wait()
	close(results)

	successes := 0
	failures := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent claims: successes=%d failures=%d", successes, failures)
	}
	if err := s.Verify(0, 0); err != nil {
		t.Fatal(err)
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Tasks["exclusive"].Owner == "" {
		t.Fatal("successful claim did not establish an owner")
	}
}
func TestArtifactExportsAndRecovery(t *testing.T) {
	s := setup(t)
	p := filepath.Join(t.TempDir(), "evidence.txt")
	_ = os.WriteFile(p, []byte("evidence"), 0600)
	ev, e := s.AddArtifact("owner", p)
	if e != nil {
		t.Fatal(e)
	}
	if len(ev.EntityID) != 64 {
		t.Fatal("artifact is not addressed")
	}
	var jsonl, md bytes.Buffer
	if e = s.ExportJSONL(&jsonl); e != nil {
		t.Fatal(e)
	}
	if e = s.ExportMarkdown(&md); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(md.String(), "Integrity: **true**") {
		t.Fatal("report missing integrity")
	}
}

func TestAgentRenameUpdatesDisplayNameAndPreservesVerification(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	if _, err := s.Execute("owner", "agent.rename", "alpha", model.AgentRenamed{DisplayName: "Alpha"}); err != nil {
		t.Fatal(err)
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents["alpha"].DisplayName != "Alpha" {
		t.Fatalf("display name was not updated: %+v", state.Agents["alpha"])
	}
	if err := s.Verify(0, 0); err != nil {
		t.Fatal(err)
	}
}

func TestAgentRenameRequiresOwnerOrOrchestrator(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	activate(t, s, "beta", model.PrincipalAgent)
	if _, err := s.Execute("beta", "agent.rename", "alpha", model.AgentRenamed{DisplayName: "Alpha"}); err == nil {
		t.Fatal("expected a non-privileged actor to be rejected")
	}
}

func TestAgentRenameRejectsUnknownAgentAndEmptyName(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	if _, err := s.Execute("owner", "agent.rename", "unknown", model.AgentRenamed{DisplayName: "Ghost"}); err == nil {
		t.Fatal("expected an error for an unregistered agent")
	}
	if _, err := s.Execute("owner", "agent.rename", "alpha", model.AgentRenamed{DisplayName: ""}); err == nil {
		t.Fatal("expected an error for an empty display name")
	}
}

// TestGrantingOrchestratorRoleRequiresHumanPrincipal guards a hard,
// deliberate escalation limit: an existing ORCHESTRATOR that is itself an
// AGENT principal (not human) must not be able to mint further
// orchestrators on its own, even though it already passes the ordinary
// owner-or-orchestrator elevation check every other agent.activate call
// requires. Every orchestrator promotion needs a human in the loop.
func TestGrantingOrchestratorRoleRequiresHumanPrincipal(t *testing.T) {
	s := setup(t)
	if _, err := s.Register("agent-lead", "Agent Lead", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	grantOrchestrator(t, s, "owner", "agent-lead", []string{"src"})

	if _, err := s.Register("candidate", "Candidate", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute("agent-lead", "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); err == nil {
		t.Fatal("expected an agent-principal orchestrator to be rejected granting the orchestrator role")
	}

	// The same agent-lead orchestrator may still grant any non-orchestrator
	// role — only the orchestrator grant itself is human-gated.
	must(t, s, "agent-lead", "agent.activate", "candidate", model.AgentActivated{Role: model.Role("MEMBER"), Scopes: []string{"src"}})

	// A human principal who is also already an orchestrator (elevation is
	// still role-based, same as ever) may grant the orchestrator role.
	if _, err := s.Register("human-lead", "Human Lead", model.PrincipalHuman); err != nil {
		t.Fatal(err)
	}
	grantOrchestrator(t, s, "owner", "human-lead", []string{"src"})
	if _, err := s.Register("second-candidate", "Second Candidate", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	grantOrchestrator(t, s, "human-lead", "second-candidate", []string{"src"})
}

// TestGrantingOrchestratorRoleRequiresPriorHumanApproval closes the gap the
// principal-type check alone leaves open: that check only verifies the
// signing credential's type, which is satisfied trivially by an unregistered
// agent operating over the ambient owner-fallback identity (see
// docs/governance.md) — a fully autonomous session, with no human ever
// deciding anything in the moment, can still be cryptographically "human".
// ORCHESTRATOR grants must additionally require a separately-approved,
// HUMAN-tier approval record for this exact id, forcing a genuine two-step
// apply-then-approve flow instead of one self-contained command.
func TestGrantingOrchestratorRoleRequiresPriorHumanApproval(t *testing.T) {
	s := setup(t)
	if _, err := s.Register("candidate", "Candidate", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}

	// The owner is a human principal and already elevated, yet the grant
	// still fails with no approval on record at all.
	if _, err := s.Execute("owner", "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); err == nil {
		t.Fatal("expected the orchestrator grant to be rejected without a prior approval")
	}

	// Applying (requesting) alone is not enough either — the request must be
	// separately approved before the grant proceeds. See RFC 0023: this must
	// live at the exact conventional ID (OrchestratorGrantApprovalID), not
	// an arbitrary caller-chosen one, or nothing will ever find it.
	approvalID := protocol.OrchestratorGrantApprovalID("candidate")
	must(t, s, "owner", "approval.request", approvalID,
		model.ApprovalRequested{Tier: "HUMAN", Action: protocol.OrchestratorGrantApprovalAction("candidate"), Reason: "promotion"})
	if _, err := s.Execute("owner", "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); err == nil {
		t.Fatal("expected the orchestrator grant to be rejected while the approval is still pending")
	}

	// An approved ORCHESTRATOR-tier (not HUMAN-tier) approval for a
	// different candidate, at that candidate's own conventional ID, must
	// not substitute — only a HUMAN-tier approval closes the gap. Uses a
	// separate principal rather than reusing "candidate"'s own ID: RFC
	// 0023's ID-scoped lookup means a wrong-tier approval has to occupy the
	// exact ID the check looks up to prove the tier gate at all (a
	// differently-ID'd approval would simply never be found, proving
	// nothing about tier specifically) — and approval.request rejects a
	// second request at an ID that already exists, so a distinct principal
	// is the only way to get a second, differently-tiered approval at *its*
	// own conventional ID without disturbing "candidate"'s still-pending one.
	if _, err := s.Register("wrong-tier-candidate", "Wrong Tier Candidate", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	wrongTierApprovalID := protocol.OrchestratorGrantApprovalID("wrong-tier-candidate")
	must(t, s, "owner", "approval.request", wrongTierApprovalID,
		model.ApprovalRequested{Tier: "ORCHESTRATOR", Action: protocol.OrchestratorGrantApprovalAction("wrong-tier-candidate"), Reason: "wrong tier"})
	must(t, s, "owner", "approval.approve", wrongTierApprovalID, model.ApprovalResponse{})
	if _, err := s.Execute("owner", "agent.activate", "wrong-tier-candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); err == nil {
		t.Fatal("expected an ORCHESTRATOR-tier approval to be rejected as insufficient for the orchestrator grant")
	}

	// Once a matching HUMAN-tier approval is actually approved, the grant
	// succeeds.
	must(t, s, "owner", "approval.approve", approvalID, model.ApprovalResponse{})
	must(t, s, "owner", "agent.activate", "candidate", model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}})

	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents["candidate"].Role != model.RoleOrchestrator {
		t.Fatalf("expected candidate to be granted the orchestrator role, got %+v", state.Agents["candidate"])
	}
}

// TestOrchestratorGrantApprovalIsConsumedOnUse guards RFC 0023's core fix:
// an approval that has already authorized one orchestrator grant must never
// authorize a second one. Reproduces the exact live scenario a real
// project's approval history surfaced -- a HUMAN-tier approval, once
// APPROVED, silently pre-authorizing every future grant of the same role to
// the same principal, with no fresh human decision required.
func TestOrchestratorGrantApprovalIsConsumedOnUse(t *testing.T) {
	s := setup(t)
	if _, err := s.Register("candidate", "Candidate", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	approvalID := protocol.OrchestratorGrantApprovalID("candidate")
	must(t, s, "owner", "approval.request", approvalID,
		model.ApprovalRequested{Tier: "HUMAN", Action: protocol.OrchestratorGrantApprovalAction("candidate"), Reason: "promotion"})
	must(t, s, "owner", "approval.approve", approvalID, model.ApprovalResponse{})
	must(t, s, "owner", "agent.activate", "candidate", model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}})

	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Approvals[approvalID].Status; got != "CONSUMED" {
		t.Fatalf("expected the approval to be CONSUMED after authorizing the grant, got %q", got)
	}

	// Self-switch away from ORCHESTRATOR (self-service, no elevation needed
	// for a non-orchestrator target -- RFC 0018) and attempt to re-grant:
	// the principal is still ACTIVE the whole time, so a failure here can
	// only be the consumed approval doing its job, not the unrelated
	// "revoked principals can never be reactivated" rule
	// (TestAgentRevokeIsTerminal) that a revoke-based version of this test
	// would have accidentally exercised instead.
	must(t, s, "candidate", "agent.switch-role", "candidate", model.AgentRoleSwitched{Role: model.Role("MEMBER")})
	if _, err := s.Execute("owner", "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); err == nil {
		t.Fatal("expected re-granting orchestrator after switching away from it to require a fresh approval, not reuse the consumed one")
	}

	// The conventional approval ID is intentionally reusable only after its
	// previous record has been consumed. A fresh request and human approval
	// must make exactly one later re-grant possible.
	must(t, s, "owner", "approval.request", approvalID,
		model.ApprovalRequested{Tier: "HUMAN", Action: protocol.OrchestratorGrantApprovalAction("candidate"), Reason: "promotion again"})
	must(t, s, "owner", "approval.approve", approvalID, model.ApprovalResponse{})
	must(t, s, "owner", "agent.activate", "candidate", model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}})

	state, err = s.State()
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Approvals[approvalID].Status; got != "CONSUMED" {
		t.Fatalf("expected the fresh approval to be CONSUMED after authorizing the re-grant, got %q", got)
	}
}

// TestAgentRevokeIsTerminal guards the core contract: revocation is a
// one-way door. A revoked principal can never act again (the general
// active-principal gate already blocks every transition), can never be
// reactivated, renamed, or suspended, and revoking it twice fails.
func TestAgentRevokeIsTerminal(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	must(t, s, "owner", "agent.revoke", "alpha", model.RuntimeStatusChanged{Reason: "left the project"})

	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents["alpha"].Status != "REVOKED" {
		t.Fatalf("agent was not revoked: %+v", state.Agents["alpha"])
	}
	if _, err = s.Execute("owner", "agent.revoke", "alpha", model.RuntimeStatusChanged{}); err == nil {
		t.Fatal("expected revoking an already-revoked principal to fail")
	}
	if _, err = s.Execute("alpha", "task.create", "task-1", model.TaskCreated{Title: "x", Repository: "local", Branch: "b", Resources: []string{"src/x"}}); err == nil {
		t.Fatal("expected a revoked principal's own action to fail via the general active() gate")
	}
	if _, err = s.Execute("owner", "agent.activate", "alpha", model.AgentActivated{Role: model.Role("MEMBER"), Scopes: []string{"src"}}); err == nil {
		t.Fatal("expected reactivating a revoked principal to fail")
	}
	if _, err = s.Execute("owner", "agent.rename", "alpha", model.AgentRenamed{DisplayName: "Alpha"}); err == nil {
		t.Fatal("expected renaming a revoked principal to fail")
	}
	if _, err = s.Execute("owner", "agent.suspend", "alpha", model.TaskStatus{}); err == nil {
		t.Fatal("expected suspending a revoked principal to fail")
	}
}

// TestAgentRevokeCascadesToOwnRuntimes proves the projection cascade
// (internal/projection/apply.go): revoking an agent also revokes its own
// registered runtimes in the same event, closing the gap where
// interactive-serve delivery only checks runtime status, never the owning
// agent's status.
func TestAgentRevokeCascadesToOwnRuntimes(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	must(t, s, "alpha", "runtime.register", "alpha-runtime", model.RuntimeRegistered{AgentID: "alpha", Connector: "MANUAL", MaxConcurrent: 1})
	must(t, s, "alpha", "runtime.heartbeat", "alpha-runtime", model.RuntimeHeartbeat{Health: "HEALTHY"})

	must(t, s, "owner", "agent.revoke", "alpha", model.RuntimeStatusChanged{Reason: "cleanup"})

	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.AgentRuntimes["alpha-runtime"].Status != "REVOKED" {
		t.Fatalf("agent's own runtime was not cascaded to REVOKED: %+v", state.AgentRuntimes["alpha-runtime"])
	}
}

// TestAgentRevokeRejectsNonElevatedActor mirrors the ordinary
// owner-or-orchestrator elevation gate already required for agent.suspend
// and runtime.revoke.
func TestAgentRevokeRejectsNonElevatedActor(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	activate(t, s, "beta", model.PrincipalAgent)
	if _, err := s.Execute("beta", "agent.revoke", "alpha", model.RuntimeStatusChanged{}); err == nil {
		t.Fatal("expected a non-elevated actor to be rejected")
	}
}

// TestAgentRevokeSelfBypassesElevation proves a plain, non-elevated agent
// can voluntarily revoke itself without owner/orchestrator elevation —
// mirroring agent.rotate-key's existing self-service bypass.
func TestAgentRevokeSelfBypassesElevation(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	must(t, s, "alpha", "agent.revoke", "alpha", model.RuntimeStatusChanged{Reason: "retiring myself"})

	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents["alpha"].Status != "REVOKED" {
		t.Fatalf("self-revocation did not take effect: %+v", state.Agents["alpha"])
	}
}

// TestAgentRevokeNeverPermitsOwnerTarget guards against ever bricking a
// project down to zero owners — no actor, including the owner itself, can
// revoke a Role == RoleOwner principal.
func TestAgentRevokeNeverPermitsOwnerTarget(t *testing.T) {
	s := setup(t)
	if _, err := s.Execute("owner", "agent.revoke", "owner", model.RuntimeStatusChanged{}); err == nil {
		t.Fatal("expected revoking the owner to be rejected, even as self-revocation")
	}
}

// TestAgentRevokeOfOrchestratorRequiresHumanActor is the revoke-side
// sibling of TestGrantingOrchestratorRoleRequiresHumanPrincipal: an
// AGENT-principal orchestrator must not be able to unilaterally revoke a
// *different* orchestrator or any human principal, only a human actor can.
func TestAgentRevokeOfOrchestratorRequiresHumanActor(t *testing.T) {
	s := setup(t)
	if _, err := s.Register("agent-lead", "Agent Lead", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	grantOrchestrator(t, s, "owner", "agent-lead", []string{"src"})
	if _, err := s.Register("other-orchestrator", "Other Orchestrator", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	grantOrchestrator(t, s, "owner", "other-orchestrator", []string{"src"})

	if _, err := s.Execute("agent-lead", "agent.revoke", "other-orchestrator", model.RuntimeStatusChanged{}); err == nil {
		t.Fatal("expected an agent-principal orchestrator to be rejected revoking another orchestrator")
	}
	must(t, s, "owner", "agent.revoke", "other-orchestrator", model.RuntimeStatusChanged{Reason: "human-approved removal"})

	activate(t, s, "bystander", model.PrincipalAgent)
	if _, err := s.Execute("agent-lead", "agent.revoke", "bystander", model.RuntimeStatusChanged{}); err != nil {
		t.Fatalf("expected an agent-principal orchestrator to revoke a plain agent: %v", err)
	}
}

// TestAgentRevokeSelfOrchestratorBypassesHumanGate proves self-revocation
// bypasses the human-only gate too, not just ordinary elevation — an
// AGENT-principal orchestrator can voluntarily retire itself.
func TestAgentRevokeSelfOrchestratorBypassesHumanGate(t *testing.T) {
	s := setup(t)
	if _, err := s.Register("agent-lead", "Agent Lead", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	grantOrchestrator(t, s, "owner", "agent-lead", []string{"src"})
	must(t, s, "agent-lead", "agent.revoke", "agent-lead", model.RuntimeStatusChanged{Reason: "stepping down"})
}

// TestRegisterRejectsDuplicateIDWithoutTouchingExistingCredential guards a
// real, live-discovered vulnerability: Register used to write a freshly
// generated credential to the store before ever validating whether the
// actor already existed, and the local filesystem append path skipped
// ValidateTransition's "principal already exists" check entirely (only the
// remote/personal/service authority backends enforced it server-side). A
// second `agent.register` for an already-registered ID didn't fail — it
// silently minted a new keypair, overwrote the existing (valid, working)
// credential with it, and appended a new ledger event replacing the
// original public key, with no way to recover the destroyed original
// credential afterward. This bit a live agent mid-session, permanently
// bricking its ability to ever sign anything again under that identity.
func TestRegisterRejectsDuplicateIDWithoutTouchingExistingCredential(t *testing.T) {
	s := setup(t)
	if _, err := s.Register("dup-test", "Dup Test", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Store.Config()
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	originalPublicKey := before.Agents["dup-test"].PublicKey
	originalCred, err := identity.ResolveCredential(s.Store.Credentials, cfg.ProjectID, "dup-test")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Register("dup-test", "Dup Test Again", model.PrincipalAgent); err == nil {
		t.Fatal("expected a second registration of an already-registered ID to fail")
	}

	after, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if after.Agents["dup-test"].PublicKey != originalPublicKey {
		t.Fatalf("ledger's registered public key changed after a rejected duplicate registration: was %q, now %q",
			originalPublicKey, after.Agents["dup-test"].PublicKey)
	}
	afterCred, err := identity.ResolveCredential(s.Store.Credentials, cfg.ProjectID, "dup-test")
	if err != nil {
		t.Fatal(err)
	}
	if afterCred.PrivateKey != originalCred.PrivateKey || afterCred.PublicKey != originalCred.PublicKey {
		t.Fatal("the original credential was overwritten despite the duplicate registration being rejected")
	}
	// The original credential must still actually work — the real-world
	// failure mode was that it silently stopped being able to sign anything.
	must(t, s, "owner", "agent.activate", "dup-test", model.AgentActivated{Role: model.Role("MEMBER"), Scopes: []string{"src"}})
	if _, err := s.Execute("dup-test", "runtime.register", "dup-test-runtime",
		model.RuntimeRegistered{AgentID: "dup-test", Connector: "MCP", MaxConcurrent: 1}); err != nil {
		t.Fatalf("original credential can no longer sign after a rejected duplicate registration: %v", err)
	}
}

// TestRegisterDoesNotHijackTheSharedLegacyActiveProfile is the regression
// test for a real, confirmed-live gap RFC 0017 alone did not close:
// Register's own convenience of defaulting a freshly-registered identity to
// "active" used to write into the shared, machine-wide legacy ActiveProfile
// field whenever no provider session was recognized -- exactly the
// session-less path an opencode-based agent always takes. Since that field
// has no scoping at all, one such agent's own convenience default silently
// became every other session-less caller's default too, including a
// human's own plain terminal -- observed live as a project's shared slot
// ending up permanently pointed at an agent instead of its human owner,
// with no further action from anyone. Register must now leave the shared
// field alone entirely when no session is recognized.
func TestRegisterDoesNotHijackTheSharedLegacyActiveProfile(t *testing.T) {
	s := setup(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CODEX_THREAD_ID", "")

	// Force the exact precondition that actually made the old code fire
	// its opportunistic default (`ActiveProfileFor(sessionID) == ""`):
	// setup(t)'s own init already claims the legacy field for "owner" in
	// most environments, which would otherwise mask this regression
	// entirely -- the real incident's project was originally initialized
	// from a real, session-scoped caller, leaving the legacy field
	// genuinely empty until a later session-less registration claimed it.
	before, err := identity.LoadUserConfig()
	if err != nil {
		t.Fatal(err)
	}
	before.ActiveProfile = ""
	if err := identity.SaveUserConfig(before); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Register("session-less-agent", "", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}

	after, err := identity.LoadUserConfig()
	if err != nil {
		t.Fatal(err)
	}
	if after.ActiveProfile != "" {
		t.Fatalf("session-less registration claimed the shared legacy active profile: got %q, want empty",
			after.ActiveProfile)
	}
}

func TestActorKeyRotationPreservesVerification(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	before, _ := s.State()
	old := before.Agents["alpha"].KeyFingerprint
	if _, err := s.RotateKey("alpha"); err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(0, 0); err != nil {
		t.Fatal(err)
	}
	after, _ := s.State()
	if after.Agents["alpha"].KeyFingerprint == old {
		t.Fatal("fingerprint did not rotate")
	}
}

func TestElevateKeyRegistersElevatedPublicKeyOnState(t *testing.T) {
	s := setup(t)
	event, err := s.ElevateKey("owner", "a strong passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "agent.elevate-key" {
		t.Fatalf("expected an agent.elevate-key event, got %q", event.Type)
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	owner := state.Agents["owner"]
	if owner.ElevatedPublicKey == "" || owner.ElevatedKeyFingerprint == "" {
		t.Fatalf("expected owner's elevated key to be recorded on state: %+v", owner)
	}
}

// TestExecuteRequiresPassphrasePromptOnceElevatedKeyIsRegistered is the
// service-layer end-to-end test for the whole feature: once an actor has an
// elevated key, both agent.activate(ORCHESTRATOR) and
// approval.approve(HUMAN tier) must go through Service.PassphrasePrompt to
// get signed -- a nil prompt fails clearly, a wrong passphrase fails
// clearly (via identity.Credential.Decrypted's AES-GCM check), and the
// correct passphrase succeeds end-to-end against the real local daemon.
func TestExecuteRequiresPassphrasePromptOnceElevatedKeyIsRegistered(t *testing.T) {
	s := setup(t)
	activate(t, s, "candidate", model.PrincipalAgent)
	if _, err := s.ElevateKey("owner", "correct passphrase"); err != nil {
		t.Fatal(err)
	}

	approvalID := protocol.OrchestratorGrantApprovalID("candidate")
	action := protocol.OrchestratorGrantApprovalAction("candidate")
	// approval.request itself is never classified as needing the elevated
	// key (only approval.approve is), so this must still succeed signed
	// with the plain primary key even though owner now has an elevated one.
	must(t, s, "owner", "approval.request", approvalID, model.ApprovalRequested{Tier: "HUMAN", Action: action, Reason: "test"})

	// No prompt wired up at all: must fail clearly, not hang or silently
	// fall back to the primary key.
	if _, err := s.Execute("owner", "approval.approve", approvalID, model.ApprovalResponse{}); err == nil {
		t.Fatal("expected approval.approve to fail with no PassphrasePrompt configured")
	} else if !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("expected a passphrase-shaped error, got: %v", err)
	}

	// Prompt wired up but returns the wrong passphrase: must fail via
	// AES-GCM authentication, not succeed with a garbage key.
	s.PassphrasePrompt = func(string) (string, error) { return "wrong passphrase", nil }
	if _, err := s.Execute("owner", "approval.approve", approvalID, model.ApprovalResponse{}); err == nil {
		t.Fatal("expected approval.approve to fail with an incorrect passphrase")
	}

	// A prompt that itself errors (e.g. the user cancelling) must propagate
	// that error rather than proceeding.
	promptErr := errors.New("user cancelled")
	s.PassphrasePrompt = func(string) (string, error) { return "", promptErr }
	if _, err := s.Execute("owner", "approval.approve", approvalID, model.ApprovalResponse{}); !errors.Is(err, promptErr) {
		t.Fatalf("expected the prompt's own error to propagate, got: %v", err)
	}

	// Correct passphrase: succeeds end-to-end, and the resulting approval
	// is genuinely APPROVED (not just "no error").
	s.PassphrasePrompt = func(string) (string, error) { return "correct passphrase", nil }
	if _, err := s.Execute("owner", "approval.approve", approvalID, model.ApprovalResponse{}); err != nil {
		t.Fatalf("expected approval.approve to succeed with the correct passphrase: %v", err)
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Approvals[approvalID].Status != "APPROVED" {
		t.Fatalf("expected the approval to be APPROVED, got %+v", state.Approvals[approvalID])
	}

	// The second sensitive transition -- the orchestrator grant itself --
	// goes through the identical PassphrasePrompt path.
	if _, err := s.Execute("owner", "agent.activate", "candidate", model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); err != nil {
		t.Fatalf("expected the orchestrator grant to succeed with the correct passphrase: %v", err)
	}
	state, err = s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents["candidate"].Role != model.RoleOrchestrator {
		t.Fatalf("expected candidate to be granted ORCHESTRATOR, got %+v", state.Agents["candidate"])
	}
}

// TestSwitchRoleSelfServiceSucceedsEndToEndWithNoElevation is the
// full-daemon end-to-end counterpart to the ValidateTransition-level
// agent.switch-role coverage in internal/protocol: a non-elevated principal
// relabels its own role with a bare Execute call, no owner/orchestrator
// action and no elevated key involved at all -- confirming the whole
// client-to-daemon round trip, not just the validator in isolation.
func TestSwitchRoleSelfServiceSucceedsEndToEndWithNoElevation(t *testing.T) {
	s := setup(t)
	activate(t, s, "builder", model.PrincipalAgent)
	if _, err := s.Execute("builder", "agent.switch-role", "builder", model.AgentRoleSwitched{Role: model.Role("Frontend-Architect")}); err != nil {
		t.Fatalf("expected a non-elevated self-switch to succeed: %v", err)
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents["builder"].Role != "Frontend-Architect" {
		t.Fatalf("expected builder's role to become Frontend-Architect, got %+v", state.Agents["builder"])
	}
	if _, err := s.Execute("builder", "agent.switch-role", "someone-else", model.AgentRoleSwitched{Role: model.Role("Tester")}); err == nil {
		t.Fatal("expected switching a different principal's role to fail end-to-end too")
	}
}

// TestSwitchRoleToOrchestratorRequiresPassphrasePromptOnceElevatedKeyIsRegistered
// is the switch-role sibling of
// TestExecuteRequiresPassphrasePromptOnceElevatedKeyIsRegistered: switching
// yourself to ORCHESTRATOR goes through the identical PassphrasePrompt path
// as being granted it via agent.activate, once you've registered your own
// elevated key.
func TestSwitchRoleToOrchestratorRequiresPassphrasePromptOnceElevatedKeyIsRegistered(t *testing.T) {
	s := setup(t)
	activate(t, s, "human-candidate", model.PrincipalHuman)
	if _, err := s.ElevateKey("human-candidate", "correct passphrase"); err != nil {
		t.Fatal(err)
	}

	approvalID := protocol.OrchestratorGrantApprovalID("human-candidate")
	action := protocol.OrchestratorGrantApprovalAction("human-candidate")
	must(t, s, "owner", "approval.request", approvalID, model.ApprovalRequested{Tier: "HUMAN", Action: action, Reason: "test"})
	must(t, s, "owner", "approval.approve", approvalID, model.ApprovalResponse{})

	// No prompt wired up: must fail clearly.
	if _, err := s.Execute("human-candidate", "agent.switch-role", "human-candidate", model.AgentRoleSwitched{Role: model.RoleOrchestrator}); err == nil {
		t.Fatal("expected agent.switch-role to fail with no PassphrasePrompt configured")
	} else if !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("expected a passphrase-shaped error, got: %v", err)
	}

	// Wrong passphrase: fails via AES-GCM authentication, not a garbage key.
	s.PassphrasePrompt = func(string) (string, error) { return "wrong passphrase", nil }
	if _, err := s.Execute("human-candidate", "agent.switch-role", "human-candidate", model.AgentRoleSwitched{Role: model.RoleOrchestrator}); err == nil {
		t.Fatal("expected agent.switch-role to fail with an incorrect passphrase")
	}

	// Correct passphrase: succeeds end-to-end, genuinely ORCHESTRATOR, not
	// just "no error."
	s.PassphrasePrompt = func(string) (string, error) { return "correct passphrase", nil }
	if _, err := s.Execute("human-candidate", "agent.switch-role", "human-candidate", model.AgentRoleSwitched{Role: model.RoleOrchestrator}); err != nil {
		t.Fatalf("expected the self-switch to orchestrator to succeed with the correct passphrase: %v", err)
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents["human-candidate"].Role != model.RoleOrchestrator {
		t.Fatalf("expected human-candidate to become ORCHESTRATOR, got %+v", state.Agents["human-candidate"])
	}
}

// TestExecuteRequiresPassphrasePromptToRevokeOrchestrator is the revoke-side
// sibling of TestExecuteRequiresPassphrasePromptOnceElevatedKeyIsRegistered:
// revoking an orchestrator is exactly as sensitive as granting the role and
// goes through the identical client-side PassphrasePrompt path.
func TestExecuteRequiresPassphrasePromptToRevokeOrchestrator(t *testing.T) {
	s := setup(t)
	activate(t, s, "candidate", model.PrincipalAgent)
	if _, err := s.ElevateKey("owner", "correct passphrase"); err != nil {
		t.Fatal(err)
	}
	s.PassphrasePrompt = func(string) (string, error) { return "correct passphrase", nil }
	grantOrchestrator(t, s, "owner", "candidate", []string{"src"})

	s.PassphrasePrompt = nil
	if _, err := s.Execute("owner", "agent.revoke", "candidate", model.RuntimeStatusChanged{}); err == nil {
		t.Fatal("expected revoking an orchestrator to fail with no PassphrasePrompt configured")
	} else if !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("expected a passphrase-shaped error, got: %v", err)
	}

	s.PassphrasePrompt = func(string) (string, error) { return "correct passphrase", nil }
	if _, err := s.Execute("owner", "agent.revoke", "candidate", model.RuntimeStatusChanged{}); err != nil {
		t.Fatalf("expected the revoke to succeed with the correct passphrase: %v", err)
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents["candidate"].Status != "REVOKED" {
		t.Fatalf("expected candidate to be revoked, got %+v", state.Agents["candidate"])
	}
}

func TestExecuteRequiresPassphrasePromptToDeleteRevokedAgent(t *testing.T) {
	s := setup(t)
	activate(t, s, "candidate", model.PrincipalAgent)
	if _, err := s.ElevateKey("owner", "correct passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute("owner", "agent.revoke", "candidate",
		model.RuntimeStatusChanged{Reason: "retired"}); err != nil {
		t.Fatalf("revoke candidate: %v", err)
	}

	if _, err := s.Execute("owner", "agent.delete", "candidate",
		model.AgentDeleted{Reason: "remove retired identity"}); err == nil {
		t.Fatal("expected deleting a revoked principal to fail with no PassphrasePrompt configured")
	} else if !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("expected a passphrase-shaped error, got: %v", err)
	}
	s.PassphrasePrompt = func(string) (string, error) { return "correct passphrase", nil }
	event, err := s.Execute("owner", "agent.delete", "candidate",
		model.AgentDeleted{Reason: "remove retired identity"})
	if err != nil {
		t.Fatalf("expected deletion to succeed with the correct passphrase: %v", err)
	}
	if event.KeyFingerprint == "" {
		t.Fatal("service result did not expose the authority-attested signing-key fingerprint")
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := state.Agents["candidate"]; exists {
		t.Fatal("deleted principal remained in state")
	}
}

// TestExecuteDoesNotPromptForRoutineActions guards against the passphrase
// prompt leaking into ordinary, non-sensitive writes once an elevated key
// exists -- the whole point of scoping this narrowly rather than
// blanket-protecting every signature.
func TestExecuteDoesNotPromptForRoutineActions(t *testing.T) {
	s := setup(t)
	if _, err := s.ElevateKey("owner", "correct passphrase"); err != nil {
		t.Fatal(err)
	}
	s.PassphrasePrompt = func(string) (string, error) {
		t.Fatal("PassphrasePrompt must not be called for a routine, non-elevated action")
		return "", nil
	}
	must(t, s, "owner", "task.create", "task-1", model.TaskCreated{
		Title: "Routine", Repository: "local", Branch: "feature", Resources: []string{"src/x"},
	})
	activate(t, s, "bystander", model.PrincipalAgent)
}

// TestExecuteRefusesWhenActorIsAmbiguous is the direct regression test for
// RFC 0017: a real, confirmed-live incident where one agent's routine
// writes (runtime.register/runtime.revoke, neither elevated-key-gated)
// were silently signed under a *different*, unrelated agent's identity,
// purely because the actor resolved through the shared, machine-wide
// legacy ActiveProfile fallback rather than anything explicit. AmbiguousActor
// is the caller-set gate (internal/app's PersistentPreRunE, shared by CLI/
// TUI/MCP) that now refuses this outright instead of silently signing.
func TestExecuteRefusesWhenActorIsAmbiguous(t *testing.T) {
	s := setup(t)
	activate(t, s, "bystander", model.PrincipalAgent)

	s.AmbiguousActor = true
	_, err := s.Execute("bystander", "runtime.register", "some-runtime", model.RuntimeRegistered{
		AgentID: "bystander", Kind: model.RuntimeKindWorker, Connector: "MCP", MaxConcurrent: 1,
	})
	if err == nil {
		t.Fatal("expected Execute to refuse an ambiguously-resolved actor")
	}
	if !strings.Contains(err.Error(), "ambiguous") && !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("error %q does not explain the refusal", err.Error())
	}

	// The gate is a pure client-side refusal to attempt the signature at
	// all -- confirm the transition genuinely never landed, not just that
	// an error came back.
	state, stateErr := s.State()
	if stateErr != nil {
		t.Fatal(stateErr)
	}
	if _, exists := state.AgentRuntimes["some-runtime"]; exists {
		t.Fatal("the refused runtime.register must not have been recorded")
	}

	// The gate must default to false and never leak into unrelated Service
	// instances/calls -- clearing it restores completely normal behavior.
	s.AmbiguousActor = false
	if _, err := s.Execute("bystander", "runtime.register", "some-runtime", model.RuntimeRegistered{
		AgentID: "bystander", Kind: model.RuntimeKindWorker, Connector: "MCP", MaxConcurrent: 1,
	}); err != nil {
		t.Fatalf("expected Execute to succeed once AmbiguousActor is cleared: %v", err)
	}
}
