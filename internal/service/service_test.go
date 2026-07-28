package service_test

import (
	"bytes"
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
	must(t, s, "owner", "agent.activate", id, model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}})
}

// grantOrchestrator drives the two-step apply-then-approve flow the
// ORCHESTRATOR role now requires: approver "applies" a HUMAN-tier approval
// for this exact grant, then separately approves it, before finally
// activating id as ORCHESTRATOR. approver must already be elevated
// (owner/orchestrator) and a HUMAN principal for the approve step to work.
func grantOrchestrator(t *testing.T, s *service.Service, approver, id string, scopes []string) {
	t.Helper()
	approvalID := id + "-orchestrator-approval"
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
	must(t, s, "agent-lead", "agent.activate", "candidate", model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}})

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
	// separately approved before the grant proceeds.
	must(t, s, "owner", "approval.request", "candidate-orchestrator-approval",
		model.ApprovalRequested{Tier: "HUMAN", Action: protocol.OrchestratorGrantApprovalAction("candidate"), Reason: "promotion"})
	if _, err := s.Execute("owner", "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); err == nil {
		t.Fatal("expected the orchestrator grant to be rejected while the approval is still pending")
	}

	// An approved ORCHESTRATOR-tier (not HUMAN-tier) approval for the same
	// action must not substitute — only a HUMAN-tier approval closes the gap.
	must(t, s, "owner", "approval.request", "candidate-orchestrator-tier-approval",
		model.ApprovalRequested{Tier: "ORCHESTRATOR", Action: protocol.OrchestratorGrantApprovalAction("candidate"), Reason: "wrong tier"})
	must(t, s, "owner", "approval.approve", "candidate-orchestrator-tier-approval", model.ApprovalResponse{})
	if _, err := s.Execute("owner", "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); err == nil {
		t.Fatal("expected an ORCHESTRATOR-tier approval to be rejected as insufficient for the orchestrator grant")
	}

	// Once a matching HUMAN-tier approval is actually approved, the grant
	// succeeds.
	must(t, s, "owner", "approval.approve", "candidate-orchestrator-approval", model.ApprovalResponse{})
	must(t, s, "owner", "agent.activate", "candidate", model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}})

	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents["candidate"].Role != model.RoleOrchestrator {
		t.Fatalf("expected candidate to be granted the orchestrator role, got %+v", state.Agents["candidate"])
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
	if _, err = s.Execute("owner", "agent.activate", "alpha", model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}}); err == nil {
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
	must(t, s, "owner", "agent.activate", "dup-test", model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}})
	if _, err := s.Execute("dup-test", "runtime.register", "dup-test-runtime",
		model.RuntimeRegistered{AgentID: "dup-test", Connector: "MCP", MaxConcurrent: 1}); err != nil {
		t.Fatalf("original credential can no longer sign after a rejected duplicate registration: %v", err)
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
