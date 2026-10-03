package codexserve

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
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
	if path := os.Getenv("AGENTCOMMS_FAKE_CODEX_START_MARKER"); path != "" {
		_ = os.WriteFile(path, []byte("started"), 0o600)
	}
	if address := os.Getenv("AGENTCOMMS_FAKE_CODEX_LIFETIME_SOCKET"); address != "" {
		connection, err := net.Dial("tcp", address)
		if err != nil {
			os.Exit(2)
		}
		_, _ = connection.Write([]byte{1})
		go func() {
			_, _ = io.Copy(io.Discard, connection)
			os.Exit(0)
		}()
	}
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
		if request.Method == os.Getenv("AGENTCOMMS_FAKE_CODEX_REJECT_METHOD") {
			fmt.Printf(`{"jsonrpc":"2.0","id":%d,"error":{"code":-32000,"message":"synthetic handshake rejection"}}`+"\n", *request.ID)
			continue
		}
		if request.Method == "initialize" && os.Getenv("AGENTCOMMS_FAKE_CODEX_IGNORE_INITIALIZE") == "1" {
			continue
		}
		switch request.Method {
		case "initialize":
			fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{}}`+"\n", *request.ID)
		case "initialized":
			// notification, no response
		case "thread/start":
			if path := os.Getenv("AGENTCOMMS_FAKE_CODEX_THREAD_PARAMS"); path != "" {
				_ = os.WriteFile(path, request.Params, 0o600)
			}
			fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"thread":{"id":"fake-thread-1"}}}`+"\n", *request.ID)
		case "thread/resume":
			if path := os.Getenv("AGENTCOMMS_FAKE_CODEX_THREAD_PARAMS"); path != "" {
				_ = os.WriteFile(path, request.Params, 0o600)
			}
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
			if path := os.Getenv("AGENTCOMMS_FAKE_CODEX_CRASH_AFTER_ACK_MARKER"); path != "" {
				if _, err := os.Stat(path); os.IsNotExist(err) {
					_ = os.WriteFile(path, []byte("crashed"), 0o600)
					os.Exit(9)
				}
			}
			if os.Getenv("AGENTCOMMS_FAKE_CODEX_TERMINAL_FAILURE") == "1" {
				fmt.Println(`{"jsonrpc":"2.0","method":"error","params":{"error":{"message":"synthetic provider rejection"},"willRetry":false}}`)
				continue
			}
			fmt.Printf(`{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"userMessage","content":[{"type":"text","text":"input %d"}]}}}`+"\n", turn)
			fmt.Printf(`{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"agentMessage","phase":"final_answer","text":"turn %d"}}}`+"\n", turn)
			if os.Getenv("AGENTCOMMS_FAKE_CODEX_EXIT_AFTER_FINAL") == "1" {
				os.Exit(0)
			}
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

func TestProcessPinsSandboxOnStartAndResume(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprint(resume), func(t *testing.T) {
			t.Setenv("AGENTCOMMS_FAKE_CODEX_PROCESS", "1")
			path := filepath.Join(t.TempDir(), "thread-params.json")
			t.Setenv("AGENTCOMMS_FAKE_CODEX_THREAD_PARAMS", path)
			config := fakeProcessConfig(t)
			config.Sandbox = "read-only"
			config.Model = "synthetic-model"
			if resume {
				config.ThreadID = "existing-thread"
			}
			process, err := Start(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = process.Close() }()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var params map[string]any
			if err := json.Unmarshal(data, &params); err != nil {
				t.Fatal(err)
			}
			if params["sandbox"] != "read-only" || params["cwd"] != config.WorkDir || params["approvalPolicy"] != "never" || params["model"] != config.Model {
				t.Fatalf("requested runtime boundary omitted: %s", data)
			}
		})
	}
}

func TestProcessFailedStartTerminatesChild(t *testing.T) {
	for _, failure := range []string{"initialize", "thread/start", "deadline"} {
		t.Run(failure, func(t *testing.T) {
			t.Setenv("AGENTCOMMS_FAKE_CODEX_PROCESS", "1")
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			t.Setenv("AGENTCOMMS_FAKE_CODEX_LIFETIME_SOCKET", listener.Addr().String())
			if failure == "deadline" {
				t.Setenv("AGENTCOMMS_FAKE_CODEX_IGNORE_INITIALIZE", "1")
			} else {
				t.Setenv("AGENTCOMMS_FAKE_CODEX_REJECT_METHOD", failure)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			process, err := Start(ctx, fakeProcessConfig(t))
			if err == nil || process != nil {
				if process != nil {
					_ = process.Close()
				}
				t.Fatalf("failed handshake returned (%v, %v)", process, err)
			}
			_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second))
			connection, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			// Closing this synthetic control socket terminates a leaked helper
			// after a failed assertion, so the regression itself cannot orphan it.
			defer connection.Close()
			_ = connection.SetReadDeadline(time.Now().Add(time.Second))
			var ready [1]byte
			if _, err := io.ReadFull(connection, ready[:]); err != nil {
				t.Fatal(err)
			}
			if _, err := connection.Read(ready[:]); err == nil {
				t.Fatal("unexpected child lifetime data")
			} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("failed Start left the provider subprocess running")
			}
		})
	}
}

func TestProcessReturnsTerminalProviderError(t *testing.T) {
	t.Setenv("AGENTCOMMS_FAKE_CODEX_PROCESS", "1")
	t.Setenv("AGENTCOMMS_FAKE_CODEX_TERMINAL_FAILURE", "1")
	process, err := Start(context.Background(), fakeProcessConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := process.Send(ctx, "synthetic prompt"); err == nil || !strings.Contains(err.Error(), "synthetic provider rejection") {
		t.Fatalf("terminal provider error must be returned instead of a deadline: %v", err)
	}
}

func TestProcessPassesAdditionalRootsOnStartAndResume(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprint(resume), func(t *testing.T) {
			t.Setenv("AGENTCOMMS_FAKE_CODEX_PROCESS", "1")
			path := filepath.Join(t.TempDir(), "thread-params.json")
			t.Setenv("AGENTCOMMS_FAKE_CODEX_THREAD_PARAMS", path)
			config := fakeProcessConfig(t)
			config.AddDirs = []string{t.TempDir(), t.TempDir()}
			if resume {
				config.ThreadID = "existing-thread"
			}
			process, err := Start(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = process.Close() }()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var params struct {
				Config map[string][]string `json:"config"`
			}
			if err := json.Unmarshal(data, &params); err != nil {
				t.Fatal(err)
			}
			roots := params.Config["sandbox_workspace_write.writable_roots"]
			if len(roots) != len(config.AddDirs) {
				t.Fatalf("additional roots omitted: %s", data)
			}
			for i, root := range roots {
				if root != config.AddDirs[i] {
					t.Fatalf("additional root changed: %s", data)
				}
			}
		})
	}
}

func TestProcessRejectsInvalidAdditionalRoots(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"relative", filepath.Join(t.TempDir(), "missing"), file} {
		config := fakeProcessConfig(t)
		config.AddDirs = []string{root}
		if err := validateProcessConfig(config); err == nil {
			t.Errorf("accepted invalid additional root %q", root)
		}
	}
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

func TestProcessRetriesAfterAcknowledgedTurnCrashes(t *testing.T) {
	t.Setenv("AGENTCOMMS_FAKE_CODEX_PROCESS", "1")
	t.Setenv("AGENTCOMMS_FAKE_CODEX_CRASH_AFTER_ACK_MARKER", filepath.Join(t.TempDir(), "crashed"))
	process, err := Start(context.Background(), fakeProcessConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	events, stopObserving := process.Subscribe()
	defer stopObserving()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := process.Send(ctx, "recover after acknowledgement")
	if err != nil || output != "turn 1" {
		t.Fatalf("acknowledged turn crash did not resume/retry: output=%q error=%v", output, err)
	}
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("restart disconnected the persistent observer")
			}
			if strings.Contains(string(event), `"text":"turn 1"`) {
				return
			}
		case <-ctx.Done():
			t.Fatal("persistent observer did not receive the recovered answer")
		}
	}
}

func TestProcessKeepsFinalAnswerBeforeExit(t *testing.T) {
	t.Setenv("AGENTCOMMS_FAKE_CODEX_PROCESS", "1")
	t.Setenv("AGENTCOMMS_FAKE_CODEX_EXIT_AFTER_FINAL", "1")
	process, err := Start(context.Background(), fakeProcessConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	pid := process.cmd.Process.Pid
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := process.Send(ctx, "return answer before exit")
	if err != nil || output != "turn 1" {
		t.Fatalf("answer immediately before exit was lost: output=%q error=%v", output, err)
	}
	if process.cmd.Process.Pid != pid {
		t.Fatal("completed turn was needlessly retried after its final answer")
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
