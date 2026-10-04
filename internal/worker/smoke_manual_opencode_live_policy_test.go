package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/opencodeclient"
)

// Actual worker + native provider seam, with no test-only policy override.
// Owned ephemeral servers and disposable projects only; no shared URL adoption.
func TestManualSmokeOpenCodeLiveWorkerPolicy(t *testing.T) {
	if os.Getenv("AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE=1 for actual native worker policy verification")
	}
	for _, mode := range []string{"plan", "manual", "auto", "dontAsk", "acceptEdits"} {
		t.Run(mode, func(t *testing.T) {
			instruction := "Use the native edit or apply_patch tool (never shell/bash) to create worker-policy-proof.txt in the project root containing exactly native-worker-proof. Make one edit attempt; if permission is denied, report that denial without retrying or using other tools. Then return a concise result. This is a disposable security test."
			instance, root := workerServiceWithInstruction(t, instruction)
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			worker, err := New(Config{Service: instance, Actor: "claude-axiom", RuntimeID: "runtime-axiom", Adapter: "opencode-live", WorkDir: root, PermissionMode: mode, ListenWait: time.Second, ExecutionTimeout: 120 * time.Second, Once: true})
			if err != nil {
				t.Fatal(err)
			}
			adapter := worker.adapter.(*openCodeLiveAdapter)
			var mu sync.Mutex
			var permissions []string
			var observerError error
			var observerDone chan struct{}
			worker.config.Status = func(status string) {
				if !strings.HasPrefix(status, "watch this runtime's OpenCode activity live") {
					return
				}
				client := opencodeclient.New(adapter.server.baseURL, adapter.workDir)
				events, err := opencodeclient.Subscribe(ctx, client)
				if err != nil {
					observerError = err
					return
				}
				observerDone = make(chan struct{})
				go func() {
					defer close(observerDone)
					for event := range events {
						if event.Type != "permission.asked" {
							continue
						}
						var request opencodeclient.PermissionRequest
						if json.Unmarshal(event.Properties, &request) == nil && request.SessionID == adapter.sessionID {
							mu.Lock()
							permissions = append(permissions, request.Permission)
							mu.Unlock()
						}
					}
				}()
			}
			runErr := worker.Run(ctx)
			if observerError != nil {
				t.Fatal(observerError)
			}
			if observerDone == nil {
				t.Fatal("worker never reported its owned attach endpoint")
			}
			cancel()
			select {
			case <-observerDone:
			case <-time.After(3 * time.Second):
				t.Fatal("owned observer did not close")
			}
			mu.Lock()
			observed := append([]string(nil), permissions...)
			mu.Unlock()
			foundEdit := false
			for _, kind := range observed {
				if kind == "edit" {
					foundEdit = true
				}
			}
			if !foundEdit {
				t.Fatalf("native worker did not emit an edit permission request: %v", observed)
			}
			proofPath := filepath.Join(root, "worker-policy-proof.txt")
			proof, readErr := os.ReadFile(proofPath)
			if mode != "acceptEdits" && !os.IsNotExist(readErr) {
				t.Fatalf("%s worker edited despite policy: error=%v", mode, readErr)
			}
			if mode == "acceptEdits" && (readErr != nil || strings.TrimSpace(string(proof)) != "native-worker-proof") {
				t.Fatalf("legitimate edit failed: %v", readErr)
			}
			state, err := instance.State()
			if err != nil {
				t.Fatal(err)
			}
			invocation := state.Invocations["inv-worker"]
			if mode != "acceptEdits" && runErr != nil {
				// Native refusal can return no text. The established worker contract
				// then records WAITING with a reason, rather than fake completion.
				if invocation.Status != "WAITING" || !strings.Contains(invocation.Reason, "permission request was denied for: edit") {
					t.Fatalf("denied native turn lacks expected durable waiting evidence: status=%s error=%v", invocation.Status, runErr)
				}
			} else if runErr != nil || invocation.Status != "COMPLETED" || invocation.ResultMessageID == "" {
				t.Fatalf("worker failed to publish result/completion: status=%s error=%v", invocation.Status, runErr)
			}
			if !adapter.closed || adapter.server != nil || adapter.runtimeLock != nil || adapter.sessionLock != nil {
				t.Fatal("one-shot worker leaked owned resources")
			}
			t.Logf("actual %s worker: edit request emitted, file expectation satisfied, durable invocation outcome recorded and owned resources closed", fmt.Sprint(mode))
		})
	}
}
