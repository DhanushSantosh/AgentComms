package claudeserve

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() {
	if os.Getenv("AGENTCOMMS_FAKE_CLAUDE_PROCESS") != "1" {
		return
	}
	marker := os.Getenv("AGENTCOMMS_FAKE_CLAUDE_CRASH_MARKER")
	if marker != "" {
		if _, err := os.Stat(marker); os.IsNotExist(err) {
			_ = os.WriteFile(marker, []byte("crashed"), 0o600)
			os.Exit(9)
		}
	}
	denied := os.Getenv("AGENTCOMMS_FAKE_CLAUDE_DENIED") == "1"
	scanner := bufio.NewScanner(os.Stdin)
	turn := 0
	for scanner.Scan() {
		turn++
		var envelope map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &envelope)
		fmt.Println(string(scanner.Bytes()))
		fmt.Printf("{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"turn %d\"}]}}\n", turn)
		if denied {
			fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"","permission_denials":[{"tool":"Bash"}]}`)
		} else {
			fmt.Printf("{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"turn %d\",\"permission_denials\":[]}\n", turn)
		}
	}
	os.Exit(0)
}

func fakeProcessConfig(t *testing.T) ProcessConfig {
	t.Helper()
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	return ProcessConfig{
		Executable: executable, WorkDir: t.TempDir(), PermissionMode: "dontAsk",
		SystemPrompt: "test", MaxBudgetUSD: 1,
	}
}

func TestProcessPersistsAcrossTurnsAndBroadcasts(t *testing.T) {
	t.Setenv("AGENTCOMMS_FAKE_CLAUDE_PROCESS", "1")
	process, err := Start(context.Background(), fakeProcessConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	events, cancel := process.Subscribe()
	defer cancel()

	first, err := process.Send(context.Background(), "first")
	if err != nil || first != "turn 1" {
		t.Fatalf("first Send() = (%q, %v)", first, err)
	}
	second, err := process.Send(context.Background(), "second")
	if err != nil || second != "turn 2" {
		t.Fatalf("second Send() = (%q, %v)", second, err)
	}

	deadline := time.After(2 * time.Second)
	found := false
	for !found {
		select {
		case event := <-events:
			found = strings.Contains(string(event), `"text":"turn 2"`)
		case <-deadline:
			t.Fatal("subscriber did not receive the second live turn")
		}
	}
}

func TestProcessRetriesOnceAfterCrash(t *testing.T) {
	t.Setenv("AGENTCOMMS_FAKE_CLAUDE_PROCESS", "1")
	t.Setenv("AGENTCOMMS_FAKE_CLAUDE_CRASH_MARKER", filepath.Join(t.TempDir(), "crashed"))
	process, err := Start(context.Background(), fakeProcessConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	output, err := process.Send(context.Background(), "recover")
	if err != nil || output != "turn 1" {
		t.Fatalf("Send() after crash = (%q, %v)", output, err)
	}
}

func TestProcessTreatsPermissionDenialAsFailure(t *testing.T) {
	t.Setenv("AGENTCOMMS_FAKE_CLAUDE_PROCESS", "1")
	t.Setenv("AGENTCOMMS_FAKE_CLAUDE_DENIED", "1")
	process, err := Start(context.Background(), fakeProcessConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	if _, err := process.Send(context.Background(), "denied"); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected a permission-denial error, got %v", err)
	}
}
