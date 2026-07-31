package codexserve

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

// The fake process below stands in for a real `codex app-server`,
// following the same self-exec-the-test-binary trick claudeserve's own
// tests use: AGENTCOMMS_FAKE_CODEX_PROCESS gates it so it never runs
// during a normal `go test` invocation of this package's real tests.
func init() {
	if os.Getenv("AGENTCOMMS_FAKE_CODEX_PROCESS") != "1" {
		return
	}
	marker := os.Getenv("AGENTCOMMS_FAKE_CODEX_CRASH_MARKER")
	scanner := bufio.NewScanner(os.Stdin)
	turn := 0
	for scanner.Scan() {
		var request struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			continue
		}
		switch request.Method {
		case "initialize":
			fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{}}`+"\n", *request.ID)
		case "initialized":
			// notification, no response
		case "thread/start":
			fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"thread":{"id":"fake-thread-1"}}}`+"\n", *request.ID)
		case "thread/resume":
			fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"thread":{"id":"fake-thread-1"}}}`+"\n", *request.ID)
		case "turn/start":
			// A crash marker means "crash on the first turn/start this
			// process instance sees" -- a mid-conversation crash, the
			// scenario worth testing recovery for. The handshake calls
			// above (initialize/thread/start) must always succeed, or
			// every restart attempt would fail before ever reaching a
			// turn, which isn't the scenario this test exercises.
			if marker != "" {
				if _, err := os.Stat(marker); os.IsNotExist(err) {
					_ = os.WriteFile(marker, []byte("crashed"), 0o600)
					os.Exit(9)
				}
			}
			turn++
			fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"turn":{"id":"turn-%d","status":"inProgress"}}}`+"\n", *request.ID, turn)
			fmt.Printf(`{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"userMessage","content":[{"type":"text","text":"input %d"}]}}}`+"\n", turn)
			fmt.Printf(`{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"agentMessage","phase":"final_answer","text":"turn %d"}}}`+"\n", turn)
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
	return ProcessConfig{Executable: executable, WorkDir: t.TempDir(), Sandbox: "workspace-write"}
}

func TestProcessPersistsAcrossTurnsAndBroadcasts(t *testing.T) {
	t.Setenv("AGENTCOMMS_FAKE_CODEX_PROCESS", "1")
	process, err := Start(context.Background(), fakeProcessConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	if process.ThreadID() != "fake-thread-1" {
		t.Fatalf("ThreadID() = %q, want fake-thread-1", process.ThreadID())
	}
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
	t.Setenv("AGENTCOMMS_FAKE_CODEX_PROCESS", "1")
	t.Setenv("AGENTCOMMS_FAKE_CODEX_CRASH_MARKER", filepath.Join(t.TempDir(), "crashed"))
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

func TestProcessResumesConfiguredThreadID(t *testing.T) {
	t.Setenv("AGENTCOMMS_FAKE_CODEX_PROCESS", "1")
	config := fakeProcessConfig(t)
	config.ThreadID = "fake-thread-1"
	process, err := Start(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	if process.ThreadID() != "fake-thread-1" {
		t.Fatalf("ThreadID() = %q, want fake-thread-1", process.ThreadID())
	}
}
