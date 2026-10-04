package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// Exercise the production adapter's native session and policy lifecycle, not
// an HTTP stand-in or a client-only permission rewrite.
func TestManualSmokeOpenCodeLiveContinuityAndNativeDeny(t *testing.T) {
	if os.Getenv("AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE=1 for actual native policy verification")
	}
	for _, scenario := range []string{"mode-change", "native-deny"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
			if scenario == "native-deny" {
				if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(`{"permission":{"edit":"deny"}}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
			defer cancel()
			adapter := &openCodeLiveAdapter{}
			defer func() {
				if err := adapter.Close(); err != nil {
					t.Error(err)
				}
			}()
			config := Config{Actor: "synthetic", RuntimeID: "continuity-owned", WorkDir: root, PermissionMode: "acceptEdits"}
			instruction := func(name string) string {
				return "Use only the native edit or apply_patch tool to create " + name + " containing exactly native-continuity-proof. Never use shell/bash or another tool. Make one attempt; if denied, report denial without retrying. This is a disposable security test."
			}
			firstOutput, firstErr := adapter.Execute(ctx, config, model.Invocation{ID: "native-first", Instruction: instruction("first-proof.txt")})
			first, readErr := os.ReadFile(filepath.Join(root, "first-proof.txt"))
			if scenario == "native-deny" {
				if !os.IsNotExist(readErr) {
					t.Fatalf("native edit deny overridden by acceptEdits: %v", readErr)
				}
				if adapter.sessionID == "" {
					t.Fatalf("deny test never reached an owned native session: %v", firstErr)
				}
				if firstErr != nil {
					t.Fatalf("native deny did not produce a completed refusal: %v", firstErr)
				}
				refusal := strings.ToLower(firstOutput)
				if !strings.Contains(refusal, "den") && !strings.Contains(refusal, "permission") {
					t.Fatal("native deny lacks an explicit refusal result")
				}
				t.Log("production acceptEdits preserved native edit deny; no file was created")
				return
			}
			if firstErr != nil || readErr != nil || strings.TrimSpace(string(first)) != "native-continuity-proof" {
				t.Fatalf("legitimate first edit failed: execution=%v read=%v", firstErr, readErr)
			}
			sessionID, endpoint := adapter.sessionID, adapter.server.baseURL
			config.PermissionMode = "plan"
			_, secondErr := adapter.Execute(ctx, config, model.Invocation{ID: "native-second", Instruction: instruction("second-proof.txt")})
			if _, err := os.Stat(filepath.Join(root, "second-proof.txt")); !os.IsNotExist(err) {
				t.Fatalf("later plan turn retained prior edit authority: %v", err)
			}
			if adapter.sessionID != sessionID {
				t.Fatal("policy change silently replaced the native conversation")
			}
			if adapter.server != nil && adapter.server.baseURL != endpoint {
				t.Fatal("successful turn switched its owned endpoint")
			}
			if secondErr != nil && !strings.Contains(secondErr.Error(), "permission request was denied for: edit") {
				t.Fatalf("second turn failed for an unrelated reason: %v", secondErr)
			}
			first, readErr = os.ReadFile(filepath.Join(root, "first-proof.txt"))
			if readErr != nil || strings.TrimSpace(string(first)) != "native-continuity-proof" {
				t.Fatal("policy reset disturbed the existing work")
			}
			t.Log("same native conversation allowed first edit then denied a plan-mode edit without disturbing existing work")
		})
	}
}

func TestManualSmokeOpenCodeLiveSameRuntimeDifferentProjects(t *testing.T) {
	if os.Getenv("AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE=1 for actual native project isolation verification")
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "shared-user"))
	first, second := &openCodeLiveAdapter{}, &openCodeLiveAdapter{}
	defer func() {
		for _, adapter := range []*openCodeLiveAdapter{first, second} {
			if err := adapter.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	firstConfig := Config{Actor: "synthetic", RuntimeID: "same-logical-runtime", WorkDir: t.TempDir(), PermissionMode: "plan"}
	secondConfig := firstConfig
	secondConfig.WorkDir = t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	for _, turn := range []struct {
		adapter *openCodeLiveAdapter
		config  Config
		marker  string
	}{{first, firstConfig, "FIRST-PROJECT-NATIVE-PROOF"}, {second, secondConfig, "SECOND-PROJECT-NATIVE-PROOF"}, {first, firstConfig, "FIRST-PROJECT-RESUMED-PROOF"}} {
		output, err := turn.adapter.Execute(ctx, turn.config, model.Invocation{ID: turn.marker, Instruction: "Reply with exactly " + turn.marker + ". Do not use tools."})
		if err != nil || !strings.Contains(output, turn.marker) {
			t.Fatalf("same-ID project-scoped native turn failed: %v", err)
		}
	}
	if first.recordPath == second.recordPath || first.sessionID == second.sessionID || first.server.baseURL == second.server.baseURL || first.workDir == second.workDir {
		t.Fatal("native workers with the same logical ID shared project-owned resources")
	}
	t.Log("same runtime ID in two projects retained distinct native endpoints, sessions and records; first project resumed successfully")
}
