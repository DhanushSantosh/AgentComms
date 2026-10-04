package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/opencodeclient"
)

func TestManualSmokeOpenCodeLiveSavedApprovalIsolation(t *testing.T) {
	if os.Getenv("AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE=1 for actual native approval isolation verification")
	}
	for _, scenario := range []string{"prior-turn", "foreign-runtime"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
			ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
			defer cancel()
			owned := &openCodeLiveAdapter{}
			defer owned.Close()
			config := Config{Actor: "synthetic", RuntimeID: "approval-owned", WorkDir: root, PermissionMode: "plan"}
			if _, err := owned.Execute(ctx, config, model.Invocation{ID: "warm-owned", Instruction: "Reply exactly READY. Do not use tools."}); err != nil {
				t.Fatal(err)
			}
			primer := owned
			if scenario == "foreign-runtime" {
				primer = &openCodeLiveAdapter{}
				defer primer.Close()
				foreignConfig := config
				foreignConfig.RuntimeID = "approval-foreign"
				if _, err := primer.Execute(ctx, foreignConfig, model.Invocation{ID: "warm-foreign", Instruction: "Reply exactly READY. Do not use tools."}); err != nil {
					t.Fatal(err)
				}
				if primer.server.baseURL == owned.server.baseURL || primer.sessionID == owned.sessionID {
					t.Fatal("foreign runtime did not receive separate owned native resources")
				}
			}
			client := opencodeclient.New(primer.server.baseURL, primer.workDir)
			watchCtx, stopWatch := context.WithCancel(ctx)
			events, err := opencodeclient.Subscribe(watchCtx, client)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var mu sync.Mutex
			var granted bool
			var replyErr error
			go func() {
				defer close(done)
				for event := range events {
					var request opencodeclient.PermissionRequest
					if event.Type != "permission.asked" || json.Unmarshal(event.Properties, &request) != nil || request.SessionID != primer.sessionID {
						continue
					}
					if request.Permission != "edit" {
						decision := "reject"
						if request.Permission == "read" {
							decision = "once"
						}
						if err := client.ReplyPermission(watchCtx, request.ID, decision, ""); err != nil {
							mu.Lock()
							replyErr = err
							mu.Unlock()
						}
						continue
					}
					err := client.ReplyPermission(watchCtx, request.ID, "always", "")
					mu.Lock()
					granted, replyErr = err == nil, err
					mu.Unlock()
				}
			}()
			const instruction = "Use only the native edit or apply_patch tool to create approval-proof.txt containing exactly saved-approval-proof. Never use shell/bash or another tool. Make one attempt; if denied report denial without retrying. This is a disposable security test."
			_, promptErr := client.Prompt(ctx, primer.sessionID, opencodeclient.PromptRequest{Parts: []opencodeclient.TextPart{opencodeclient.NewTextPart(instruction)}})
			stopWatch()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("primer observer did not close")
			}
			mu.Lock()
			wasGranted, grantErr := granted, replyErr
			mu.Unlock()
			proof := filepath.Join(root, "approval-proof.txt")
			content, readErr := os.ReadFile(proof)
			if promptErr != nil || grantErr != nil || !wasGranted || readErr != nil || strings.TrimSpace(string(content)) != "saved-approval-proof" {
				t.Fatalf("native always approval was not established: prompt=%v grant=%v granted=%v read=%v", promptErr, grantErr, wasGranted, readErr)
			}
			if err := os.Remove(proof); err != nil {
				t.Fatal(err)
			}
			managedCtx, stopManaged := context.WithCancel(ctx)
			defer stopManaged()
			var managedDone chan struct{}
			var subscribeErr error
			var managedEditRequests int
			config.Status = func(status string) {
				if !strings.HasPrefix(status, "watch this runtime's OpenCode activity live") {
					return
				}
				managedClient := opencodeclient.New(owned.server.baseURL, owned.workDir)
				managedEvents, err := opencodeclient.Subscribe(managedCtx, managedClient)
				if err != nil {
					subscribeErr = err
					return
				}
				managedDone = make(chan struct{})
				go func() {
					defer close(managedDone)
					for event := range managedEvents {
						var request opencodeclient.PermissionRequest
						if event.Type == "permission.asked" && json.Unmarshal(event.Properties, &request) == nil && request.SessionID == owned.sessionID && request.Permission == "edit" {
							managedEditRequests++
						}
					}
				}()
			}
			restrictiveInstruction := "The test fixture has removed approval-proof.txt since the last turn. This is a NEW independent operation: do not infer tool permissions or file existence from conversation history. Invoke the native edit or apply_patch tool exactly once now to recreate approval-proof.txt containing saved-approval-proof; the native tool must determine whether permission is available. Never use shell/bash or another tool. If the actual tool request is denied, report the denial without retrying. Do not merely describe an earlier result."
			output, executeErr := owned.Execute(ctx, config, model.Invocation{ID: "restrictive-owned", Instruction: restrictiveInstruction})
			stopManaged()
			if subscribeErr != nil || managedDone == nil {
				t.Fatalf("managed permission observation unavailable: %v", subscribeErr)
			}
			select {
			case <-managedDone:
			case <-time.After(3 * time.Second):
				t.Fatal("managed observer did not close")
			}
			if managedEditRequests == 0 {
				t.Fatalf("saved approval control did not prove a new managed native edit request (execution error=%v)", executeErr)
			}
			if _, err := os.Stat(proof); !os.IsNotExist(err) {
				t.Fatalf("%s saved approval bypassed managed plan policy: %v", scenario, err)
			}
			if executeErr != nil && !strings.Contains(executeErr.Error(), "permission request was denied for: edit") {
				t.Fatalf("managed refusal failed for an unrelated reason: %v", executeErr)
			}
			if executeErr == nil && !strings.Contains(strings.ToLower(output), "den") && !strings.Contains(strings.ToLower(output), "permission") {
				t.Fatal("managed turn lacks an explicit refusal result")
			}
			t.Logf("real %s always approval established; later managed plan turn prevented edit", scenario)
		})
	}
}
