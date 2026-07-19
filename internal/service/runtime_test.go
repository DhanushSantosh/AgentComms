package service

import (
	"testing"

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
