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

	"github.com/DhanushSantosh/AgentComms/internal/opencodeclient"
)

func TestManualSmokeOpenCodeLiveWorkerReadAndGovernance(t *testing.T) {
	if os.Getenv("AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE=1 for actual native worker policy verification")
	}
	for _, kind := range []string{"read", "bash", "external_directory"} {
		t.Run(kind, func(t *testing.T) {
			const readToken = "NATIVE-READ-PROOF"
			const externalToken = "NATIVE-EXTERNAL-CONTENT-MUST-NOT-LEAK"
			instruction := "Use only the native read tool to read read-proof.txt in the project root. Return its exact content. Do not use shell/bash."
			if kind == "bash" {
				instruction = "Use only the native bash tool to run: printf native-shell-proof > denied-shell.txt . Make one attempt; if denied, report denial without retries or other tools. This is a disposable security test."
			}
			if kind == "external_directory" {
				outside := filepath.Join(t.TempDir(), "external-proof.txt")
				if err := os.WriteFile(outside, []byte(externalToken), 0600); err != nil {
					t.Fatal(err)
				}
				instruction = "Use only the native read tool to read the file at " + outside + " . Make one attempt; if denied, report denial without retries or other tools. This is a disposable security test."
			}
			instance, root := workerServiceWithInstruction(t, instruction)
			if kind == "read" {
				if err := os.WriteFile(filepath.Join(root, "read-proof.txt"), []byte(readToken), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			worker, err := New(Config{Service: instance, Actor: "claude-axiom", RuntimeID: "runtime-axiom", Adapter: "opencode-live", WorkDir: root, PermissionMode: "acceptEdits", ListenWait: time.Second, ExecutionTimeout: 120 * time.Second, Once: true})
			if err != nil {
				t.Fatal(err)
			}
			adapter := worker.adapter.(*openCodeLiveAdapter)
			var mu sync.Mutex
			var observed []string
			var observeErr error
			var done chan struct{}
			worker.config.Status = func(status string) {
				if !strings.HasPrefix(status, "watch this runtime's OpenCode activity live") {
					return
				}
				client := opencodeclient.New(adapter.server.baseURL, adapter.workDir)
				events, err := opencodeclient.Subscribe(ctx, client)
				if err != nil {
					observeErr = err
					return
				}
				sessionID := adapter.sessionID
				done = make(chan struct{})
				go func() {
					defer close(done)
					for event := range events {
						if event.Type != "permission.asked" {
							continue
						}
						var request opencodeclient.PermissionRequest
						if json.Unmarshal(event.Properties, &request) == nil && request.SessionID == sessionID {
							mu.Lock()
							observed = append(observed, request.Permission)
							mu.Unlock()
						}
					}
				}()
			}
			runErr := worker.Run(ctx)
			if observeErr != nil || done == nil {
				t.Fatalf("native observation unavailable: %v", observeErr)
			}
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("native observer did not close")
			}
			mu.Lock()
			kinds := append([]string(nil), observed...)
			mu.Unlock()
			found := false
			for _, permission := range kinds {
				if permission == kind {
					found = true
				}
			}
			if !found {
				t.Fatalf("native worker did not emit required permission %s: %v", kind, kinds)
			}
			state, err := instance.State()
			if err != nil {
				t.Fatal(err)
			}
			invocation := state.Invocations["inv-worker"]
			result := state.Messages[invocation.ResultMessageID].Body
			if kind == "read" {
				if runErr != nil || invocation.Status != "COMPLETED" || !strings.Contains(result, readToken) {
					t.Fatalf("legitimate native read/completion failed: status=%s error=%v", invocation.Status, runErr)
				}
			} else {
				if runErr != nil && (invocation.Status != "WAITING" || !strings.Contains(invocation.Reason, "permission request was denied for: "+kind)) {
					t.Fatalf("governance refusal lacks durable evidence: status=%s error=%v", invocation.Status, runErr)
				}
				if runErr == nil && (invocation.Status != "COMPLETED" || invocation.ResultMessageID == "") {
					t.Fatal("native refusal outcome was not published")
				}
				if kind == "bash" {
					if _, err := os.Stat(filepath.Join(root, "denied-shell.txt")); !os.IsNotExist(err) {
						t.Fatalf("governed shell executed: %v", err)
					}
				}
				if kind == "external_directory" && strings.Contains(result, externalToken) {
					t.Fatal("external read content leaked despite governance denial")
				}
			}
			if !adapter.closed || adapter.server != nil || adapter.runtimeLock != nil || adapter.sessionLock != nil {
				t.Fatal("native one-shot worker leaked owned resources")
			}
			t.Logf("actual native %s permission observed and expected durable outcome verified", kind)
		})
	}
}
