package claudeserve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeBrokerServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/health" {
			http.NotFound(response, request)
			return
		}
		response.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestServerInfoRoundTrips(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	path, err := ServerInfoPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := ServerInfo{BaseURL: "http://127.0.0.1:4097"}
	if err := saveServerInfo(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadServerInfo(path)
	if err != nil || got != want {
		t.Fatalf("loadServerInfo() = (%+v, %v), want %+v", got, err, want)
	}
}

func TestResolveRunningServerPrefersCache(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	cached := fakeBrokerServer(t)
	fallback := fakeBrokerServer(t)
	path, err := ServerInfoPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := saveServerInfo(path, ServerInfo{BaseURL: cached.URL}); err != nil {
		t.Fatal(err)
	}
	baseURL, ok := resolveRunningServer(context.Background(), path, fallback.URL)
	if !ok || baseURL != cached.URL {
		t.Fatalf("resolveRunningServer() = (%q, %v), want cached %q", baseURL, ok, cached.URL)
	}
}

func TestResolveRunningServerFallsBackToFixedPort(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	fallback := fakeBrokerServer(t)
	path, err := ServerInfoPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	baseURL, ok := resolveRunningServer(context.Background(), path, fallback.URL)
	if !ok || baseURL != fallback.URL {
		t.Fatalf("resolveRunningServer() = (%q, %v), want fallback %q", baseURL, ok, fallback.URL)
	}
}

func TestResolveRunningServerReportsNone(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	path, err := ServerInfoPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := resolveRunningServer(context.Background(), path, "http://127.0.0.1:1"); ok {
		t.Fatal("expected no running broker")
	}
}

func TestBrokerRejectsConflictingRuntimeRegistration(t *testing.T) {
	t.Setenv("AGENTCOMMS_FAKE_CLAUDE_PROCESS", "1")
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	broker := NewBroker()
	defer broker.Close()
	server := httptest.NewServer(broker.Handler())
	defer server.Close()
	client := New(server.URL)
	config := ProcessConfig{
		Executable: executable, WorkDir: t.TempDir(), PermissionMode: "dontAsk",
		SystemPrompt: "one", MaxBudgetUSD: 1,
	}
	if err := client.Register(context.Background(), "runtime-one", config); err != nil {
		t.Fatal(err)
	}
	config.SystemPrompt = "different"
	if err := client.Register(context.Background(), "runtime-one", config); err == nil {
		t.Fatal("expected conflicting registration to fail")
	}
}

func TestBrokerPromptAndLiveSubscription(t *testing.T) {
	t.Setenv("AGENTCOMMS_FAKE_CLAUDE_PROCESS", "1")
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	broker := NewBroker()
	defer broker.Close()
	server := httptest.NewServer(broker.Handler())
	defer server.Close()
	client := New(server.URL)
	config := ProcessConfig{
		Executable: executable, WorkDir: t.TempDir(), PermissionMode: "dontAsk",
		SystemPrompt: "test", MaxBudgetUSD: 1,
	}
	if err := client.Register(context.Background(), "runtime-live", config); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := client.Subscribe(ctx, "runtime-live")
	if err != nil {
		t.Fatal(err)
	}
	output, err := client.Prompt(context.Background(), "runtime-live", "hello")
	if err != nil || output != "turn 1" {
		t.Fatalf("Prompt() = (%q, %v)", output, err)
	}
	for {
		select {
		case event := <-events:
			if strings.Contains(string(event), `"text":"turn 1"`) {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("live subscriber did not receive the assistant turn")
		}
	}
}

func TestServerInfoPathNeverPointsInsideProjectRoot(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	path, err := ServerInfoPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(path, root) {
		t.Fatalf("ServerInfoPath returned a path inside root: %s", path)
	}
}
