package worker

import (
	"os"
	"path/filepath"
	"testing"
)

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
	for _, builtIn := range []string{"agy", "claude", "codex", "opencode", "claude-acp"} {
		spec := DeclarativeSpec{Name: builtIn, ExecutableName: builtIn}
		if err := RegisterDeclarativeAdapter(spec); err == nil {
			t.Fatalf("expected RegisterDeclarativeAdapter to refuse overwriting built-in adapter %q", builtIn)
		}
	}
}
