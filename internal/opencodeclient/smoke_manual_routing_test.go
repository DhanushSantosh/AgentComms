package opencodeclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// This opt-in uses only an owned ephemeral server, never the user's shared
// port 4096. It deliberately inherits the production server's permission
// configuration: forcing ask here would conceal a request-emission gap.
func TestManualSmokeOpenCodeLiveRoutingAndPolicy(t *testing.T) {
	if os.Getenv("AGENTCOMMS_OPENCODE_LIVE_ROUTING_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_OPENCODE_LIVE_ROUTING_SMOKE=1 for owned real-provider verification")
	}
	runOwnedNativePolicyProbe(t, nativePolicyProbe{})
}

// These mechanism controls are diagnostic evidence, not a production fix.
// In particular, the inherited-approval control must demonstrate the unsafe
// edit, so passing this test cannot certify the live adapter's policy.
func TestManualSmokeOpenCodeNativePermissionMechanics(t *testing.T) {
	if os.Getenv("AGENTCOMMS_OPENCODE_NATIVE_POLICY_MECHANICS") != "1" {
		t.Skip("set AGENTCOMMS_OPENCODE_NATIVE_POLICY_MECHANICS=1 for owned provider mechanism controls")
	}
	for name, probe := range map[string]nativePolicyProbe{
		"ask-only":                           {forceAsk: true},
		"foreign-always-bypasses-ask":        {forceAsk: true, seedAlways: true, expectEdit: true},
		"owned-instance-reset-clears-always": {forceAsk: true, seedAlways: true, resetInstance: true},
		"native-deny-survives-acceptEdits":   {forceAsk: true, nativeDeny: true},
	} {
		t.Run(name, func(t *testing.T) { runOwnedNativePolicyProbe(t, probe) })
	}
}

type nativePolicyProbe struct{ forceAsk, seedAlways, resetInstance, expectEdit, nativeDeny bool }

func runOwnedNativePolicyProbe(t *testing.T, probe nativePolicyProbe) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	root := t.TempDir()
	cmd := exec.CommandContext(ctx, "opencode", "serve", "--hostname", "127.0.0.1", "--port", "0", "--pure")
	cmd.Dir = root
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready, drained := make(chan string, 1), make(chan struct{})
	go func() {
		defer close(drained)
		scanner := bufio.NewScanner(stdout)
		found := false
		for scanner.Scan() {
			if address, ok := ParseListeningURL(scanner.Text()); ok && !found {
				found = true
				ready <- address
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		// Wait only after draining stdout, as required by StdoutPipe.
		select {
		case <-drained:
		case <-time.After(5 * time.Second):
			_ = stdout.Close()
			<-drained
		}
		_ = cmd.Wait()
		if cmd.ProcessState == nil {
			t.Error("owned provider was not reaped")
		}
	})
	var baseURL string
	select {
	case baseURL = <-ready:
	case <-ctx.Done():
		t.Fatal("owned server startup timed out")
	}
	if _, err := waitUntilHealthy(ctx, baseURL); err != nil {
		t.Fatal(err)
	}
	otherRoot := t.TempDir()
	client, other := New(baseURL, root), New(baseURL, otherRoot)
	var spec struct {
		Paths map[string]map[string]struct {
			RequestBody json.RawMessage `json:"requestBody"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := client.do(ctx, http.MethodGet, "/doc", nil, &spec); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/session", "/session/{sessionID}/message"} {
		schema := spec.Paths[path]["post"].RequestBody
		if len(schema) > 6000 {
			schema = schema[:6000]
		}
		t.Logf("native request schema %s: %s", path, schema)
	}
	t.Logf("native permission ruleset schema: %s", spec.Components.Schemas["PermissionRuleset"])
	t.Logf("native permission rule schema: %s", spec.Components.Schemas["PermissionRule"])
	t.Logf("native session patch schema: %s", spec.Paths["/session/{sessionID}"]["patch"].RequestBody)
	owned, err := client.CreateSession(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if probe.forceAsk {
		// Seed existing conversation content without executing a model turn.
		// Preparation must delete its own marker, not clear the conversation.
		if err := client.do(ctx, http.MethodPost, "/session/"+owned.ID+"/message", map[string]any{"noReply": true, "parts": []TextPart{NewTextPart("Synthetic history-retention control; no action requested.")}}, nil); err != nil {
			t.Fatal(err)
		}
		type historyMessage struct {
			Info  struct{ ID string } `json:"info"`
			Parts []Part              `json:"parts"`
		}
		var before []historyMessage
		if err := client.do(ctx, http.MethodGet, "/session/"+owned.ID+"/message", nil, &before); err != nil || len(before) != 1 {
			t.Fatalf("cannot establish existing history control: %v", err)
		}
		if probe.nativeDeny {
			owned, err = client.AppendSessionPermissions(ctx, owned.ID, []PermissionRule{{Permission: "edit", Pattern: "*", Action: "deny"}})
			if err != nil {
				t.Fatal(err)
			}
		}
		original := append([]PermissionRule(nil), owned.Permission...)
		// Native PATCH appends rules. Exercise bounded no-reply preparation on
		// this owned fixture to clear old managed rules without deleting history.
		markerID, err := NewPreparationID()
		if err != nil {
			t.Fatal(err)
		}
		marker, err := client.CreatePreparation(ctx, owned.ID, markerID)
		if err != nil {
			t.Fatal(err)
		}
		agent, err := client.ResolveAgent(ctx, marker.Agent)
		if err != nil {
			t.Fatal(err)
		}
		rules, err := RestrictivePermissions(agent.Permission, original)
		if err != nil {
			t.Fatal(err)
		}
		owned, err = client.AppendSessionPermissions(ctx, owned.ID, rules)
		if err != nil {
			t.Fatal(err)
		}
		readback, err := client.GetSession(ctx, owned.ID)
		expected := append([]PermissionRule{{Permission: "*", Pattern: "*", Action: "deny"}}, rules...)
		if err != nil || !reflect.DeepEqual(readback.Permission, expected) {
			t.Fatalf("native policy readback differs: want_count=%d got_count=%d error=%v", len(expected), len(readback.Permission), err)
		}
		if err := client.RemovePreparation(ctx, owned.ID, marker.ID); err != nil {
			t.Fatal(err)
		}
		var after []historyMessage
		if err := client.do(ctx, http.MethodGet, "/session/"+owned.ID+"/message", nil, &after); err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("preparation changed existing message identities/content or retained its marker: %v", err)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := other.CreateSession(ctx, otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	if owned.ID == "" || owned.ID == foreign.ID || owned.Directory != root || foreign.Directory != otherRoot {
		t.Fatal("provider failed distinct project/session routing")
	}
	resumed, err := client.GetSession(ctx, owned.ID)
	if err != nil || resumed.ID != owned.ID || resumed.Directory != root {
		t.Fatalf("resume routing failed: %v", err)
	}
	primerID := ""
	if probe.seedAlways {
		primer, err := client.CreateSession(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		primerID = primer.ID
		if err := client.do(ctx, http.MethodPatch, "/session/"+primerID, map[string]any{"permission": []map[string]string{{"permission": "*", "pattern": "*", "action": "ask"}}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	events, err := Subscribe(watchCtx, client)
	if err != nil {
		t.Fatal(err)
	}
	watcher := NewPermissionWatcher(client, owned.ID, func() bool { return probe.nativeDeny }, &fixedApprover{})
	done := make(chan struct{})
	forwarded := make(chan Event)
	var mu sync.Mutex
	var observed []string
	go func() {
		defer close(forwarded)
		for event := range events {
			if event.Type == "permission.asked" {
				var request PermissionRequest
				err := decodeInto(event.Properties, &request)
				mu.Lock()
				observed = append(observed, fmt.Sprintf("category=%q owned=%v decode_error=%v", request.Permission, request.SessionID == owned.ID, err))
				mu.Unlock()
				if primerID != "" && request.SessionID == primerID {
					if err := client.ReplyPermission(ctx, request.ID, "always", ""); err != nil {
						t.Error(err)
					}
					continue
				}
			}
			select {
			case forwarded <- event:
			case <-watchCtx.Done():
				return
			}
		}
	}()
	go func() { defer close(done); watcher.Run(watchCtx, forwarded) }()
	defer func() { cancel(); <-done }()
	if primerID != "" {
		_, err := client.Prompt(ctx, primerID, PromptRequest{Parts: []TextPart{NewTextPart("Use the write/edit tool to create the relative file AGC_OWNED_PERMISSION_PROBE.txt containing exactly synthetic primer. Do not use shell commands.")}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(root, "AGC_OWNED_PERMISSION_PROBE.txt")); err != nil {
			t.Fatal(err)
		}
	}
	if probe.resetInstance {
		cancelWatch()
		<-done
		if err := client.DisposeInstance(ctx); err != nil {
			t.Fatal(err)
		}
		events, err := Subscribe(ctx, client)
		if err != nil {
			t.Fatal(err)
		}
		watcher = NewPermissionWatcher(client, owned.ID, func() bool { return false }, &fixedApprover{})
		done = make(chan struct{})
		go func() { defer close(done); watcher.Run(ctx, events) }()
	}
	_, err = client.Prompt(ctx, owned.ID, PromptRequest{Parts: []TextPart{NewTextPart("Use the write/edit tool to create the relative file AGC_OWNED_PERMISSION_PROBE.txt containing exactly synthetic probe. Do not use shell commands. If permission is denied, stop and report the denial without an alternative tool.")}})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	requests := append([]string(nil), observed...)
	mu.Unlock()
	t.Logf("native permission request observations: %v", requests)
	var pending []PermissionRequest
	if err := client.do(ctx, http.MethodGet, "/permission", nil, &pending); err != nil {
		t.Fatal(err)
	}
	t.Logf("pending permission count after prompt: %d", len(pending))
	if probe.expectEdit {
		if _, err := os.Stat(filepath.Join(root, "AGC_OWNED_PERMISSION_PROBE.txt")); err != nil {
			t.Fatalf("inherited-approval bypass control did not reproduce: %v", err)
		}
		if watcher.Denied() {
			t.Fatal("bypass control unexpectedly reached owned denial")
		}
		t.Log("unsafe inherited approval reproduced; this is not policy enforcement")
		return
	}
	if _, err := os.Stat(filepath.Join(root, "AGC_OWNED_PERMISSION_PROBE.txt")); !os.IsNotExist(err) {
		t.Fatalf("plan-mode edit escaped the watcher (file stat: %v, denied kinds: %v)", err, watcher.DeniedKinds())
	}
	if !watcher.Denied() && !probe.nativeDeny {
		t.Fatal("real provider emitted no denied permission request; policy coverage unproven")
	}
	t.Log("real directory/create/resume routing and owned edit denial verified")
}
