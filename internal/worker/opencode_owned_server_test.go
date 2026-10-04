package worker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/opencodeclient"
)

func TestOpenCodeOwnedServerHelper(t *testing.T) {
	if os.Getenv("AGC_OWNED_SERVER_HELPER") != "1" {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := "http://" + listener.Addr().String()
	if os.Getenv("AGC_OWNED_SERVER_BAD_ADDRESS") == "1" {
		address = "http://0.0.0.0:4096"
	}
	if os.Getenv("AGC_OWNED_SERVER_NO_READY") != "1" {
		fmt.Printf("opencode server listening on %s\n", address)
		// The owner must drain logs after readiness, not stop its reader.
		fmt.Print(strings.Repeat("synthetic log\n", 10000))
	}
	server := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `[]`)
	})}
	if err := server.Serve(listener); err != nil {
		t.Fatal(err)
	}
}

func ownedServerTestCommand(t *testing.T, extra ...string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestOpenCodeOwnedServerHelper$")
	cmd.Env = append(os.Environ(), append([]string{"AGC_OWNED_SERVER_HELPER=1"}, extra...)...)
	cmd.Dir = t.TempDir()
	return cmd
}

func TestOwnedOpenCodeServerCloseIsScopedAndReaped(t *testing.T) {
	unrelated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "alive") }))
	defer unrelated.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := ownedServerTestCommand(t)
	server, err := startOwnedLiveServer(ctx, cmd, cmd.Dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	if err := opencodeclient.New(server.baseURL, cmd.Dir).Health(ctx); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := server.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	if cmd.ProcessState == nil {
		t.Fatal("owned server was not reaped")
	}
	if err := opencodeclient.New(server.baseURL, cmd.Dir).Health(ctx); err == nil {
		t.Fatal("closed server still listening")
	}
	response, err := http.Get(unrelated.URL)
	if err != nil {
		t.Fatal("unrelated server was affected", err)
	}
	response.Body.Close()
}

func TestOwnedOpenCodeStartupFailuresAreReaped(t *testing.T) {
	for _, option := range []string{"AGC_OWNED_SERVER_BAD_ADDRESS=1", "AGC_OWNED_SERVER_NO_READY=1"} {
		t.Run(option, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			cmd := ownedServerTestCommand(t, option)
			server, err := startOwnedLiveServer(ctx, cmd, cmd.Dir)
			if err == nil || server != nil {
				t.Fatal("invalid startup accepted")
			}
			if cmd.ProcessState == nil {
				t.Fatal("failed startup provider not reaped")
			}
		})
	}
}

func TestOwnedCancellationKeepsCleanupFailure(t *testing.T) {
	synthetic := errors.New("synthetic tree cleanup failure")
	if err := withoutOwnedCancellation(errors.Join(context.Canceled, synthetic)); !errors.Is(err, synthetic) {
		t.Fatal("cleanup failure hidden", err)
	}
	if err := withoutOwnedCancellation(errors.Join(context.Canceled, nil)); err != nil {
		t.Fatal("expected cancellation returned", err)
	}
}
