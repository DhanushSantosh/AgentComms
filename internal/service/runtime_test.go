package service

import (
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestRuntimeRegistrationPresenceAndLifecycle(t *testing.T) {
	instance := setup(t)
	activate(t, instance, "builder", model.PrincipalAgent)

	must(t, instance, "builder", "runtime.register", "runtime-1", model.RuntimeRegistered{
		AgentID: "builder", Connector: "mcp", ConfigReference: "profiles/builder-mcp",
		MaxConcurrent: 2, Scopes: []string{"src"}, Capabilities: []string{"review"},
	})
	must(t, instance, "builder", "runtime.heartbeat", "runtime-1", model.RuntimeHeartbeat{
		Health: "healthy",
	})
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	runtime := state.AgentRuntimes["runtime-1"]
	if runtime.Status != "ONLINE" || runtime.Health != "HEALTHY" ||
		runtime.Connector != "MCP" || runtime.LastSeenAt.IsZero() {
		t.Fatalf("unexpected online runtime: %+v", runtime)
	}

	must(t, instance, "owner", "runtime.drain", "runtime-1", model.RuntimeStatusChanged{
		Reason: "maintenance",
	})
	must(t, instance, "owner", "runtime.resume", "runtime-1", model.RuntimeStatusChanged{})
	must(t, instance, "owner", "runtime.revoke", "runtime-1", model.RuntimeStatusChanged{
		Reason: "retired",
	})
	state, err = instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.AgentRuntimes["runtime-1"].Status != "REVOKED" {
		t.Fatalf("runtime was not revoked: %+v", state.AgentRuntimes["runtime-1"])
	}
	if _, err = instance.Execute("builder", "runtime.heartbeat", "runtime-1",
		model.RuntimeHeartbeat{Health: "HEALTHY"}); err == nil {
		t.Fatal("revoked runtime accepted a heartbeat")
	}
}

func TestInvocationPolicyControlsAgentToAgentRequests(t *testing.T) {
	instance := setup(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	activate(t, instance, "alpha", model.PrincipalAgent)
	activate(t, instance, "beta", model.PrincipalAgent)

	if _, err := instance.Execute("alpha", "invocation.request", "manual-denied", model.InvocationRequested{
		Target: "builder", Instruction: "Run without approval",
	}); err == nil {
		t.Fatal("unapproved invocation bypassed the default manual policy")
	}

	must(t, instance, "owner", "invocation.policy.update", "builder", model.InvocationPolicyUpdated{
		Mode: "TRUSTED", TrustedActors: []string{"alpha"}, AllowedScopes: []string{"src"},
		RequireHumanForSensitive: true,
	})
	must(t, instance, "alpha", "invocation.request", "trusted-allowed", model.InvocationRequested{
		Target: "builder", Instruction: "Review the source tree",
	})
	if _, err := instance.Execute("beta", "invocation.request", "trusted-denied", model.InvocationRequested{
		Target: "builder", Instruction: "Review the source tree",
	}); err == nil {
		t.Fatal("untrusted actor bypassed a trusted invocation policy")
	}

	must(t, instance, "owner", "invocation.policy.update", "builder", model.InvocationPolicyUpdated{
		Mode: "DISABLED",
	})
	if _, err := instance.Execute("alpha", "invocation.request", "disabled-denied", model.InvocationRequested{
		Target: "builder", Instruction: "Run while disabled",
	}); err == nil {
		t.Fatal("disabled invocation policy accepted a request")
	}
}

func TestRuntimeConfigReferenceRejectsSecretMaterial(t *testing.T) {
	instance := setup(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	if _, err := instance.Execute("builder", "runtime.register", "runtime-secret", model.RuntimeRegistered{
		AgentID: "builder", Connector: "WEBHOOK",
		ConfigReference: "https://example.invalid?token=plaintext", MaxConcurrent: 1,
	}); err == nil {
		t.Fatal("runtime registration persisted apparent secret material")
	}
}

func TestNextInvocationPrioritizesUrgencyThenAgeAndHonorsCapacity(t *testing.T) {
	instance := setup(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	must(t, instance, "owner", "runtime.register", "runtime-queue", model.RuntimeRegistered{
		AgentID: "builder", Connector: "MCP", MaxConcurrent: 1,
	})
	must(t, instance, "owner", "invocation.request", "normal", model.InvocationRequested{
		Target: "builder", Instruction: "Normal work", Priority: "NORMAL",
	})
	must(t, instance, "owner", "invocation.request", "urgent", model.InvocationRequested{
		Target: "builder", Instruction: "Urgent work", Priority: "URGENT",
	})

	next, found, err := instance.NextInvocation("builder", "runtime-queue")
	if err != nil {
		t.Fatal(err)
	}
	if !found || next.ID != "urgent" {
		t.Fatalf("next invocation=%+v found=%t, want urgent", next, found)
	}

	must(t, instance, "builder", "invocation.claim", "urgent", model.InvocationClaimed{RuntimeID: "runtime-queue"})
	must(t, instance, "builder", "runtime.heartbeat", "runtime-queue", model.RuntimeHeartbeat{
		Health: "HEALTHY", ActiveInvocations: []string{"urgent"},
	})
	if _, found, err = instance.NextInvocation("builder", "runtime-queue"); err != nil || found {
		t.Fatalf("capacity-bound runtime returned work: found=%t err=%v", found, err)
	}
}

func TestListenInvocationReceivesNewWorkWithoutPolling(t *testing.T) {
	instance := setup(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	must(t, instance, "owner", "runtime.register", "runtime-listener", model.RuntimeRegistered{
		AgentID: "builder", Connector: "MCP", MaxConcurrent: 1,
	})
	type result struct {
		invocation model.Invocation
		found      bool
		err        error
	}
	delivered := make(chan result, 1)
	go func() {
		invocation, found, err := instance.ListenInvocation("builder", "runtime-listener", time.Second)
		delivered <- result{invocation: invocation, found: found, err: err}
	}()
	time.Sleep(50 * time.Millisecond)
	must(t, instance, "owner", "invocation.request", "pushed", model.InvocationRequested{
		Target: "builder", Instruction: "Act on pushed work", Priority: "HIGH",
	})
	select {
	case received := <-delivered:
		if received.err != nil || !received.found || received.invocation.ID != "pushed" {
			t.Fatalf("unexpected listen result: %+v", received)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("connected runtime did not receive invocation")
	}
}

func TestInvocationPolicyEnforcesScopesAndSensitiveApproval(t *testing.T) {
	instance := setup(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	activate(t, instance, "alpha", model.PrincipalAgent)
	must(t, instance, "owner", "invocation.policy.update", "builder", model.InvocationPolicyUpdated{
		Mode: "AUTOMATIC", AllowedScopes: []string{"src/api"}, RequireHumanForSensitive: true,
	})
	if _, err := instance.Execute("alpha", "invocation.request", "scope-denied", model.InvocationRequested{
		Target: "builder", Instruction: "Modify a UI file", Scopes: []string{"src/ui"},
	}); err == nil {
		t.Fatal("invocation exceeded the target policy scopes")
	}
	must(t, instance, "alpha", "invocation.request", "scope-allowed", model.InvocationRequested{
		Target: "builder", Instruction: "Review an API file", Scopes: []string{"src/api"},
	})
	must(t, instance, "owner", "task.create", "sensitive-task", model.TaskCreated{
		Title: "Sensitive change", Repository: "local", Branch: "sensitive",
		Resources: []string{"src/api"}, Risk: "HIGH",
	})
	if _, err := instance.Execute("alpha", "invocation.request", "sensitive-denied", model.InvocationRequested{
		Target: "builder", TaskID: "sensitive-task", Instruction: "Perform sensitive work",
	}); err == nil {
		t.Fatal("sensitive invocation bypassed human approval")
	}
	must(t, instance, "owner", "approval.request", "approve-sensitive", model.ApprovalRequested{
		Tier: "HUMAN", Action: "invocation-sensitive:sensitive-approved", Reason: "approved by user",
	})
	must(t, instance, "owner", "approval.approve", "approve-sensitive", model.ApprovalResponse{})
	must(t, instance, "alpha", "invocation.request", "sensitive-approved", model.InvocationRequested{
		Target: "builder", TaskID: "sensitive-task", Instruction: "Perform sensitive work",
	})
}

func TestRuntimePresenceExpiresWithoutHeartbeat(t *testing.T) {
	now := time.Now().UTC()
	state := model.State{AgentRuntimes: map[string]model.AgentRuntime{
		"fresh": {
			ID: "fresh", Status: "ONLINE",
			Health: "HEALTHY", LastSeenAt: now.Add(-controlplane.RuntimeOfflineAfter / 2),
		},
		"stale": {
			ID: "stale", Status: "ONLINE",
			Health: "HEALTHY", LastSeenAt: now.Add(-controlplane.RuntimeOfflineAfter * 2),
		},
		"draining": {
			ID: "draining", Status: "DRAINING",
			Health: "HEALTHY", LastSeenAt: now.Add(-controlplane.RuntimeOfflineAfter * 2),
		},
	}}
	RefreshRuntimePresence(&state, now)
	if state.AgentRuntimes["fresh"].Status != "ONLINE" {
		t.Fatal("fresh runtime was marked offline")
	}
	if state.AgentRuntimes["stale"].Status != "OFFLINE" ||
		state.AgentRuntimes["stale"].Reason != "heartbeat expired" {
		t.Fatalf("stale runtime was not expired: %+v", state.AgentRuntimes["stale"])
	}
	if state.AgentRuntimes["draining"].Status != "DRAINING" {
		t.Fatal("draining runtime status was overwritten")
	}
}
