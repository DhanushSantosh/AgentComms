package opencodeclient

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
			if address, ok := parseListeningURL(scanner.Text()); ok && !found {
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
	owned, err := client.CreateSession(ctx, root)
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
	events, err := Subscribe(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	watcher := NewPermissionWatcher(client, owned.ID, func() bool { return false }, &fixedApprover{})
	done := make(chan struct{})
	go func() { defer close(done); watcher.Run(ctx, events) }()
	defer func() { cancel(); <-done }()
	_, err = client.Prompt(ctx, owned.ID, PromptRequest{Parts: []TextPart{NewTextPart("Use the write/edit tool to create the relative file AGC_OWNED_PERMISSION_PROBE.txt containing exactly synthetic probe. Do not use shell commands. If permission is denied, stop and report the denial without an alternative tool.")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "AGC_OWNED_PERMISSION_PROBE.txt")); !os.IsNotExist(err) {
		t.Fatalf("plan-mode edit escaped the watcher (file stat: %v, denied kinds: %v)", err, watcher.DeniedKinds())
	}
	if !watcher.Denied() {
		t.Fatal("real provider emitted no denied permission request; policy coverage unproven")
	}
	t.Log("real directory/create/resume routing and owned edit denial verified")
}
