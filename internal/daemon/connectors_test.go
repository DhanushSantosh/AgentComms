package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

type submittedCommand struct {
	Actor     string
	EventType string
	EntityID  string
	Payload   any
}

func TestDispatcherReservesThenLaunchesOnce(t *testing.T) {
	var submitted []submittedCommand
	dispatcher, err := NewDispatcher(dispatcherConfigs(t), func(_ context.Context, _ string, actor, eventType, entityID string, payload any) error {
		submitted = append(submitted, submittedCommand{Actor: actor, EventType: eventType, EntityID: entityID, Payload: payload})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	launches := 0
	dispatcher.launch = func(_ context.Context, config ConnectorConfig, envelope InvocationEnvelope) error {
		launches++
		if config.Type != "LOCAL_PROCESS" || envelope.Invocation.ID != "inv-1" {
			t.Fatalf("unexpected connector launch: config=%+v envelope=%+v", config, envelope)
		}
		return nil
	}
	state := dispatcherState()
	if err = dispatcher.Dispatch(context.Background(), "project", state); err != nil {
		t.Fatal(err)
	}
	if launches != 1 || len(submitted) != 2 ||
		submitted[0].EventType != "invocation.delivery-attempt" ||
		submitted[1].EventType != "invocation.notify" {
		t.Fatalf("launches=%d submitted=%+v", launches, submitted)
	}
}

func TestDispatcherRecordsRetryableFailure(t *testing.T) {
	var submitted []submittedCommand
	dispatcher, err := NewDispatcher(dispatcherConfigs(t), func(_ context.Context, _ string, actor, eventType, entityID string, payload any) error {
		submitted = append(submitted, submittedCommand{Actor: actor, EventType: eventType, EntityID: entityID, Payload: payload})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher.launch = func(context.Context, ConnectorConfig, InvocationEnvelope) error {
		return errors.New("runtime failed to start")
	}
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	dispatcher.now = func() time.Time { return now }
	if err = dispatcher.Dispatch(context.Background(), "project", dispatcherState()); err != nil {
		t.Fatal(err)
	}
	if len(submitted) != 2 || submitted[0].EventType != "invocation.delivery-attempt" ||
		submitted[1].EventType != "invocation.delivery-failed" {
		t.Fatalf("unexpected delivery commands: %+v", submitted)
	}
	failure := submitted[1].Payload.(model.InvocationDeliveryFailed)
	if failure.Final || failure.NextRetry == nil || !failure.NextRetry.After(now) {
		t.Fatalf("unexpected retryable failure: %+v", failure)
	}
}

func TestNonDeliveryConnectorsCannotManufactureSuccess(t *testing.T) {
	envelope := InvocationEnvelope{
		ProjectID:  "project",
		Invocation: model.Invocation{ID: "invocation", Target: "builder"},
		Runtime:    model.AgentRuntime{ID: "runtime", AgentID: "builder"},
	}
	for _, connector := range []string{"MANUAL", "MCP"} {
		t.Run(connector, func(t *testing.T) {
			if err := launchConnector(context.Background(), ConnectorConfig{Type: connector}, envelope); err == nil {
				t.Fatalf("%s connector reported a delivery it cannot prove", connector)
			}
		})
	}
}

func TestLocalProcessConnectorReceivesBoundedEnvelope(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "envelope.json")
	config := ConnectorConfig{
		Type: "LOCAL_PROCESS", Executable: os.Args[0],
		Arguments: []string{"-test.run=TestConnectorHelperProcess", "--"},
		Environment: map[string]string{
			"CONNECTOR_TEST_HELPER": "1",
			"CONNECTOR_TEST_OUTPUT": outputPath,
		},
		Timeout: 5 * time.Second,
	}
	envelope := InvocationEnvelope{
		ProjectID: "project", Invocation: model.Invocation{ID: "inv-process", Target: "builder"},
		Runtime: model.AgentRuntime{ID: "runtime-process", AgentID: "builder"},
	}
	if err := launchConnector(context.Background(), config, envelope); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	var received InvocationEnvelope
	if err = json.Unmarshal(raw, &received); err != nil {
		t.Fatal(err)
	}
	if received.ProjectID != envelope.ProjectID || received.Invocation.ID != envelope.Invocation.ID ||
		received.Runtime.ID != envelope.Runtime.ID {
		t.Fatalf("connector received unexpected envelope: %+v", received)
	}
}

func TestWebhookConnectorPushesInvocation(t *testing.T) {
	received := make(chan InvocationEnvelope, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer relay-token" {
			http.Error(response, "invalid relay request", http.StatusBadRequest)
			return
		}
		var envelope InvocationEnvelope
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		received <- envelope
		response.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	config := ConnectorConfig{
		Type: "WEBHOOK", Endpoint: server.URL,
		Headers: map[string]string{"Authorization": "Bearer relay-token"},
		Timeout: time.Second,
	}
	if err := validateConnectorConfig("builder-webhook", config); err != nil {
		t.Fatal(err)
	}
	envelope := InvocationEnvelope{
		ProjectID:  "project",
		Invocation: model.Invocation{ID: "inv-webhook", Target: "builder"},
		Runtime:    model.AgentRuntime{ID: "runtime-webhook", AgentID: "builder"},
	}
	if err := launchConnector(context.Background(), config, envelope); err != nil {
		t.Fatal(err)
	}
	select {
	case delivered := <-received:
		if delivered.Invocation.ID != envelope.Invocation.ID || delivered.Runtime.ID != envelope.Runtime.ID {
			t.Fatalf("unexpected webhook envelope: %+v", delivered)
		}
	case <-time.After(time.Second):
		t.Fatal("webhook invocation was not delivered")
	}
}

func TestWebhookConnectorRejectsInsecureRemoteEndpoint(t *testing.T) {
	err := validateConnectorConfig("remote", ConnectorConfig{
		Type: "WEBHOOK", Endpoint: "http://example.com/invocations",
	})
	if err == nil {
		t.Fatal("insecure remote webhook endpoint was accepted")
	}
}

func TestLoadConnectorConfigsRequiresPrivateFileAndAbsoluteExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connectors.json")
	config := `{"connectors":{"builder":{"type":"LOCAL_PROCESS","executable":"relative-agent","timeout":"5s"}}}`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConnectorConfigs(path); err == nil {
		t.Fatal("relative connector executable was accepted")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"connectors": map[string]any{
		"builder": map[string]any{"type": "LOCAL_PROCESS", "executable": executable, "timeout": "5s"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err = os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConnectorConfigs(path); err == nil {
			t.Fatal("world-readable connector configuration was accepted")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := LoadConnectorConfigs(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded["builder"].Executable != executable {
		t.Fatalf("unexpected connector config: %+v", loaded["builder"])
	}
}

func TestDispatcherRevalidatesConnectorSource(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "connectors.json")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"connectors": map[string]any{
		"local": map[string]any{
			"type": "LOCAL_PROCESS", "executable": executable, "timeout": "5s",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	configs, err := LoadConnectorConfigs(configPath)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := NewDispatcher(configs,
		func(context.Context, string, string, string, string, any) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	dispatcher.SetConfigSource(configPath)
	if err = dispatcher.ValidateRuntime("LOCAL_PROCESS", "local", ""); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if err = dispatcher.ValidateRuntime("LOCAL_PROCESS", "local", ""); err == nil {
		t.Fatal("removed connector configuration remained usable from stale memory")
	}
}

func TestConnectorHelperProcess(t *testing.T) {
	if os.Getenv("CONNECTOR_TEST_HELPER") != "1" {
		return
	}
	var envelope InvocationEnvelope
	if err := json.NewDecoder(os.Stdin).Decode(&envelope); err != nil {
		os.Exit(2)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		os.Exit(3)
	}
	if err = os.WriteFile(os.Getenv("CONNECTOR_TEST_OUTPUT"), raw, 0o600); err != nil {
		os.Exit(4)
	}
	os.Exit(0)
}

func dispatcherState() model.State {
	return model.State{
		Invocations: map[string]model.Invocation{
			"inv-1": {
				ID: "inv-1", Target: "builder", Status: "PENDING", Priority: "NORMAL",
				ConsumerMode: model.ConsumerModeWorkerOnly,
			},
		},
		InvocationDeliveries: map[string]model.InvocationDelivery{},
		AgentRuntimes: map[string]model.AgentRuntime{
			"runtime-1": {
				ID: "runtime-1", AgentID: "builder", Kind: model.RuntimeKindWorker,
				Connector: "LOCAL_PROCESS", ConfigReference: "runtime-local",
				Status: "OFFLINE", MaxConcurrent: 1,
			},
		},
	}
}

func dispatcherConfigs(t *testing.T) map[string]ConnectorConfig {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return map[string]ConnectorConfig{
		"runtime-local": {Type: "LOCAL_PROCESS", Executable: executable, Timeout: time.Second},
	}
}
