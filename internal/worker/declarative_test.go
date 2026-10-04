package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestDeclarativePromptDistinguishesExecutorAndRequester(t *testing.T) {
	adapter := declarativeAdapter{spec: DeclarativeSpec{Name: "synthetic"}}
	prompt := adapter.Prompt("executor", model.Invocation{RequestedBy: "requester", Instruction: "synthetic instruction"})
	if !strings.Contains(prompt, "runtime for agent executor.") {
		t.Fatal("executor identity missing")
	}
	if !strings.Contains(prompt, "Requester: requester\n") || strings.Contains(prompt, "Requester: executor\n") {
		t.Fatal("requester replaced with executor")
	}
}

func TestDeclarativeAdapterAgyEquivalence(t *testing.T) {
	spec := DeclarativeSpec{
		Name:                            "agy-declarative",
		ExecutableName:                  "agy",
		DefaultPermissionMode:           "acceptEdits",
		ValidPermissionModes:            []string{"acceptEdits", "accept-edits", "auto", "dontAsk", "manual", "plan"},
		DisallowClaudeAllowAgentComms:   true,
		DisallowClaudeAllowErrorMessage: "agy has no per-tool permission scoping to apply --claude-allow-agent-comms to; omit that flag for this adapter",
		BaseArgs:                        []string{"--print", "--output-format", "text"},
		PermissionModeArgs: map[string][]string{
			"dontAsk":      {"--dangerously-skip-permissions"},
			"auto":         {"--dangerously-skip-permissions"},
			"plan":         {"--mode", "plan"},
			"acceptEdits":  {"--mode", "accept-edits"},
			"accept-edits": {"--mode", "accept-edits"},
		},
		SessionIDFlag:  "--conversation",
		ModelFlag:      "--model",
		SessionEnvVars: []string{"ANTIGRAVITY_SESSION_ID", "AGY_SESSION_ID"},
	}

	if err := RegisterDeclarativeAdapter(spec); err != nil {
		t.Fatal(err)
	}

	adapter, err := resolveAdapter("agy-declarative")
	if err != nil {
		t.Fatal(err)
	}

	cliAdap, ok := adapter.(cliAdapter)
	if !ok {
		t.Fatal("expected declarative adapter to implement cliAdapter")
	}

	bin := filepath.Join(t.TempDir(), "fake-agy")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Executable: bin, PermissionMode: "acceptEdits", SessionID: "sess-123", Model: "gemini-2.5"}
	if err := adapter.Validate(&cfg); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	args := cliAdap.Arguments(cfg)
	expectedArgs := []string{"--print", "--output-format", "text", "--mode", "accept-edits", "--conversation", "sess-123", "--model", "gemini-2.5"}
	if len(args) != len(expectedArgs) {
		t.Fatalf("got args %v, want %v", args, expectedArgs)
	}
	for i, arg := range args {
		if arg != expectedArgs[i] {
			t.Fatalf("args[%d] = %q, want %q", i, arg, expectedArgs[i])
		}
	}
}

func TestLoadDeclarativeAdaptersFromDir(t *testing.T) {
	dir := t.TempDir()
	jsonContent := `{
		"name": "custom-cli",
		"executable_name": "custom",
		"base_args": ["--quiet"],
		"session_id_flag": "--session"
	}`
	if err := os.WriteFile(filepath.Join(dir, "custom.json"), []byte(jsonContent), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadDeclarativeAdaptersFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Name != "custom-cli" {
		t.Fatalf("loaded = %v, want name custom-cli", loaded)
	}

	adapter, err := resolveAdapter("custom-cli")
	if err != nil {
		t.Fatal(err)
	}
	cliAdap := adapter.(cliAdapter)
	args := cliAdap.Arguments(Config{SessionID: "s-1"})
	if len(args) != 3 || args[0] != "--quiet" || args[1] != "--session" || args[2] != "s-1" {
		t.Fatalf("custom-cli args = %v", args)
	}
}

func TestRegisterDeclarativeAdapterRefusesToOverwriteBuiltIn(t *testing.T) {
	for _, builtIn := range []string{"claude", "codex", "opencode", "claude-acp"} {
		spec := DeclarativeSpec{Name: builtIn, ExecutableName: builtIn}
		if err := RegisterDeclarativeAdapter(spec); err == nil {
			t.Fatalf("expected RegisterDeclarativeAdapter to refuse overwriting built-in adapter %q", builtIn)
		}
	}
}

// TestRegisterDeclarativeAdapterAcceptsAgy confirms agy is no longer a
// built-in name -- removed 2026-08-08 over an unresolved third-party ToS
// compliance question (see docs/backlog.md) -- so it's now available for a
// project to define its own declarative spec under, exactly like any other
// non-built-in CLI provider. TestDeclarativeAdapterAgyEquivalence already
// proves the declarative system can fully replicate its real argument
// shape; this confirms the name itself is actually free to register.
func TestRegisterDeclarativeAdapterAcceptsAgy(t *testing.T) {
	if err := RegisterDeclarativeAdapter(DeclarativeSpec{Name: "agy", ExecutableName: "agy"}); err != nil {
		t.Fatalf("expected agy to be registrable now that it isn't built-in, got: %v", err)
	}
}

// Registering a declarative adapter must NOT widen RFC 0039's provider
// set. Adapters load in the CLI process while agent IDs are validated in
// the authority process, so a provider registered here would be accepted by
// the CLI and rejected by the daemon -- verified end to end before this was
// removed. The set is fixed at build time so every process agrees.
func TestRegisteringADeclarativeAdapterDoesNotWidenTheProviderSet(t *testing.T) {
	const name = "housecat"
	t.Cleanup(func() { delete(adapters, name) })
	if err := RegisterDeclarativeAdapter(DeclarativeSpec{Name: name, ExecutableName: "housecat"}); err != nil {
		t.Fatal(err)
	}
	if model.IsKnownProvider(name) {
		t.Fatalf("%q must not become a provider: the authority process would still reject it", name)
	}
	if err := model.ValidateAgentActorID(name + "-main"); err == nil {
		t.Fatalf("%s-main must stay invalid, or the CLI accepts what the daemon refuses", name)
	}
}
