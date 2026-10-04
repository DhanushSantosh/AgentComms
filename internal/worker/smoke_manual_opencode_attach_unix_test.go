//go:build unix

package worker

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/creack/pty"
)

// Real native terminal client, not an HTTP approximation of attach. It stays
// attached while the production adapter resets/prepares a second turn.
func TestManualSmokeOpenCodeLiveAttachAcrossReset(t *testing.T) {
	if os.Getenv("AGENTCOMMS_OPENCODE_LIVE_ATTACH_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_OPENCODE_LIVE_ATTACH_SMOKE=1 for actual native terminal attach verification")
	}
	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", root+"/user")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	adapter := &openCodeLiveAdapter{}
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Error(err)
		}
	}()
	config := Config{Actor: "synthetic", RuntimeID: "attach-owned", WorkDir: root, PermissionMode: "plan", Status: func(string) {}}
	const first = "AGC-ATTACH-FIRST-PROOF"
	const second = "AGC-ATTACH-SECOND-PROOF"
	output, err := adapter.Execute(ctx, config, model.Invocation{ID: "attach-first", Instruction: "Reply with exactly " + first + ". Do not use tools."})
	if err != nil || !strings.Contains(output, first) {
		t.Fatalf("initial native turn failed: %v", err)
	}
	endpoint := adapter.server.baseURL
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(master, &pty.Winsize{Rows: 30, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	attachCtx, stopAttach := context.WithCancel(ctx)
	defer stopAttach()
	arguments := []string{"attach", endpoint, "--dir", adapter.workDir, "--session", adapter.sessionID}
	if os.Getenv("AGENTCOMMS_OPENCODE_LIVE_ATTACH_UI") == "mini" {
		arguments = append(arguments, "--mini")
	}
	command := exec.Command("opencode", arguments...)
	command.Dir = adapter.workDir
	command.Env = append(os.Environ(), "TERM=xterm-256color")
	command.Stdin, command.Stdout, command.Stderr = slave, slave, slave
	done := make(chan error, 1)
	go func() { done <- runOwnedCommand(attachCtx, command) }()
	transcript := &boundedBuffer{limit: 256 * 1024}
	drainDone := make(chan struct{})
	go func() { defer close(drainDone); _, _ = io.Copy(transcript, master) }()
	defer func() {
		stopAttach()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) && err != nil {
				t.Errorf("owned attach cleanup: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("native attach process was not reaped")
		}
		slave.Close()
		master.Close()
		select {
		case <-drainDone:
		case <-time.After(3 * time.Second):
			t.Error("owned terminal output did not drain")
		}
	}()
	waitVisible := func(marker string) {
		t.Helper()
		deadline := time.NewTimer(15 * time.Second)
		defer deadline.Stop()
		for {
			if strings.Contains(transcript.String(), marker) {
				return
			}
			select {
			case err := <-done:
				done <- err
				t.Fatalf("native attach exited before observing a turn: %v", err)
			case <-deadline.C:
				t.Fatalf("native attach did not render %s (captured bytes=%d)", marker, len(transcript.String()))
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	waitVisible(first)
	output, err = adapter.Execute(ctx, config, model.Invocation{ID: "attach-second", Instruction: "Reply with exactly " + second + ". Do not use tools."})
	if err != nil || !strings.Contains(output, second) {
		t.Fatalf("second native turn failed after reset: %v", err)
	}
	if adapter.server.baseURL != endpoint {
		t.Fatal("owned endpoint changed across successful turns")
	}
	waitVisible(second)
	t.Log("actual native attach retained its endpoint and rendered both turns across owned instance reset")
}
