package worker

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// Intercept the synthetic executable before provider argv reaches Go's test
// flag parser. The ordinary suite follows m.Run unchanged.
func TestMain(m *testing.M) {
	if role := os.Getenv("AGC_OWNED_EXEC_HELPER"); role != "" {
		ownedExecHelper(role)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func ownedExecHelper(role string) {
	if role == "large-output" {
		fmt.Print(strings.Repeat("x", maxAgentOutputBytes+1))
		return
	}
	if role == "large-failure" {
		fmt.Fprint(os.Stderr, strings.Repeat("Ω", maxAgentOutputBytes))
		os.Exit(3)
	}
	if role == "success" {
		fmt.Print("synthetic result")
		return
	}
	if role == "failure" {
		fmt.Fprint(os.Stderr, "synthetic failure")
		os.Exit(3)
	}
	go func() { time.Sleep(20 * time.Second); os.Exit(4) }()
	if role == "root" || role == "child" {
		next := "child"
		if role == "child" {
			next = "grandchild"
		}
		child := exec.Command(os.Args[0])
		child.Env = ownedHelperEnv(next, os.Getenv("AGC_OWNED_EXEC_ADDRESS"))
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(5)
		}
	}
	conn, err := net.Dial("tcp", os.Getenv("AGC_OWNED_EXEC_ADDRESS"))
	if err != nil {
		os.Exit(6)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte{role[0]}); err != nil {
		os.Exit(7)
	}
	_, _ = io.Copy(conn, conn) // echo proves a surviving unrelated process
}

func ownedHelperEnv(role, address string) []string {
	var env []string
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "AGC_OWNED_EXEC_HELPER=") && !strings.HasPrefix(value, "AGC_OWNED_EXEC_ADDRESS=") {
			env = append(env, value)
		}
	}
	return append(env, "AGC_OWNED_EXEC_HELPER="+role, "AGC_OWNED_EXEC_ADDRESS="+address)
}

func acceptOwnedHelper(t *testing.T, listener *net.TCPListener) net.Conn {
	t.Helper()
	if err := listener.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var ready [1]byte
	if _, err := io.ReadFull(conn, ready[:]); err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestOwnedExecCancelsDescendantsButNotUnrelatedProcess(t *testing.T) {
	for _, adapter := range []string{"codex", "opencode"} {
		t.Run(adapter, func(t *testing.T) {
			listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			unrelated := exec.Command(executable)
			unrelated.Env = ownedHelperEnv("unrelated", listener.Addr().String())
			if err := unrelated.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() })
			unrelatedConn := acceptOwnedHelper(t, listener)
			t.Setenv("AGC_OWNED_EXEC_HELPER", "root")
			t.Setenv("AGC_OWNED_EXEC_ADDRESS", listener.Addr().String())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				cfg := Config{Executable: executable, WorkDir: t.TempDir()}
				var err error
				if adapter == "codex" {
					_, err = runCLIAdapter(ctx, cfg, codexAdapter{}, model.Invocation{})
				} else {
					_, _, err = runOpenCode(ctx, cfg, model.Invocation{}, "")
				}
				result <- err
			}()
			owned := []net.Conn{acceptOwnedHelper(t, listener), acceptOwnedHelper(t, listener), acceptOwnedHelper(t, listener)}
			cancel()
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("cancelled provider succeeded")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("owned execution did not return within cleanup bound")
			}
			for _, conn := range owned {
				_ = conn.SetReadDeadline(time.Now().Add(time.Second))
				var b [1]byte
				if _, err := conn.Read(b[:]); err == nil {
					t.Fatal("owned process still responded")
				} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
					t.Fatal("owned descendant retained connection")
				}
			}
			_ = unrelatedConn.SetDeadline(time.Now().Add(time.Second))
			if _, err := unrelatedConn.Write([]byte{'!'}); err != nil {
				t.Fatal("unrelated process was terminated", err)
			}
			var pong [1]byte
			if _, err := io.ReadFull(unrelatedConn, pong[:]); err != nil || pong[0] != '!' {
				t.Fatal("unrelated process did not remain live", err)
			}
		})
	}
}

func TestOwnedExecSuccessFailureAndCancelledBeforeStart(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"success", "failure"} {
		command := exec.Command(executable)
		command.Env = ownedHelperEnv(role, "")
		var stdout, stderr strings.Builder
		command.Stdout, command.Stderr = &stdout, &stderr
		err := runOwnedCommand(context.Background(), command)
		if role == "success" && (err != nil || stdout.String() != "synthetic result") {
			t.Fatalf("success lost: %v %q", err, stdout.String())
		}
		if role == "failure" && (err == nil || stderr.String() != "synthetic failure") {
			t.Fatalf("failure lost: %v %q", err, stderr.String())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := exec.Command(executable)
	if err := runOwnedCommand(ctx, command); err != context.Canceled || command.Process != nil {
		t.Fatalf("cancel-before-start: %v process=%v", err, command.Process)
	}
}

func TestOwnedExecCancellationRecordsWaitingWithoutResult(t *testing.T) {
	instance, root := workerService(t)
	worker := newTestWorker(t, instance, root)
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("AGC_OWNED_EXEC_HELPER", "root")
	t.Setenv("AGC_OWNED_EXEC_ADDRESS", listener.Addr().String())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- worker.Run(ctx) }()
	owned := []net.Conn{acceptOwnedHelper(t, listener), acceptOwnedHelper(t, listener), acceptOwnedHelper(t, listener)}
	cancel()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "agent execution failed:") || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("expected cancellation failure, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not finish cancellation")
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	invocation := state.Invocations["inv-worker"]
	if invocation.Status != "WAITING" || invocation.ResultMessageID != "" || !strings.Contains(invocation.Reason, "agent execution failed:") || len(invocation.Reason) > maxFailureReasonBytes {
		t.Fatalf("cancellation state: %+v", invocation)
	}
	for id := range state.Messages {
		if strings.HasPrefix(id, "result-inv-worker-") {
			t.Fatal("cancelled invocation published a result")
		}
	}
	for _, conn := range owned {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		var b [1]byte
		if _, err := conn.Read(b[:]); err == nil {
			t.Fatal("owned process remained live")
		} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
			t.Fatal("owned process retained connection")
		}
	}
}

func TestOwnedExecPreservesOutputAndDiagnosticBounds(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, adapter := range []string{"codex", "opencode"} {
		for _, role := range []string{"large-output", "large-failure"} {
			t.Run(adapter+"/"+role, func(t *testing.T) {
				t.Setenv("AGC_OWNED_EXEC_HELPER", role)
				cfg := Config{Executable: executable, WorkDir: t.TempDir()}
				var err error
				if adapter == "codex" {
					_, err = runCLIAdapter(context.Background(), cfg, codexAdapter{}, model.Invocation{})
				} else {
					_, _, err = runOpenCode(context.Background(), cfg, model.Invocation{}, "")
				}
				if err == nil {
					t.Fatal("oversized output/failure accepted")
				}
				if role == "large-output" && !strings.Contains(err.Error(), "agent output exceeded") {
					t.Fatal(err)
				}
				if role == "large-failure" && (len(err.Error()) > maxFailureReasonBytes || !utf8.ValidString(err.Error())) {
					t.Fatal("failure diagnostic bounds lost")
				}
			})
		}
	}
}
