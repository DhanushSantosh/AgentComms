package service_test

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/daemonclient"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/runtimeinit"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

// roundTripFunc adapts a function to http.RoundTripper -- the same trivial,
// no-real-networking technique internal/daemonclient's own
// TestNewWithTransportRoundTrip uses, mirrored here so NewWithRemote can be
// exercised without a real Unix socket or on-disk daemon.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// TestNewWithRemoteConstructsUsableService proves NewWithRemote produces a
// Service that actually exercises the supplied remote client and Store --
// not just a struct literal -- by driving a real State() call through it.
// This is the exact shape the WASM demo entrypoint (this constructor's only
// real caller) needs: a Store opened locally, and a daemonclient.Client
// built via NewWithTransport against an in-process handler, with no on-disk
// config read or real socket dial involved in the construction itself
// (unlike New/NewTolerant, which both require exactly that).
func TestNewWithRemoteConstructsUsableService(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(root, "credentials"))
	if _, err := runtimeinit.Initialize(t.Context(), runtimeinit.Config{
		ProjectRoot: root, Owner: "owner", Mode: "personal",
	}); err != nil {
		t.Fatal(err)
	}
	projectStore := store.Open(root)
	config, err := projectStore.Config()
	if err != nil {
		t.Fatal(err)
	}

	wantPath := "/v1/projects/" + config.ProjectID + "/state"
	var gotPath string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		gotPath = request.URL.Path
		body, marshalErr := json.Marshal(map[string]any{
			"state": model.State{},
			"metadata": controlplane.ResultMetadata{
				Consistency: "PERSONAL_AUTHORITATIVE", Connectivity: "LOCAL",
			},
		})
		if marshalErr != nil {
			return nil, marshalErr
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Header:     make(http.Header),
		}, nil
	})
	remote, err := daemonclient.NewWithTransport("in-process", time.Second, transport)
	if err != nil {
		t.Fatalf("NewWithTransport: %v", err)
	}

	instance := service.NewWithRemote(projectStore, remote)

	state, err := instance.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if gotPath != wantPath {
		t.Fatalf("remote client was not actually used: got request path %q, want %q", gotPath, wantPath)
	}
	if state.Integrity.Consistency != "PERSONAL_AUTHORITATIVE" {
		t.Fatalf("unexpected state integrity: %+v", state.Integrity)
	}
	if instance.Store != projectStore {
		t.Fatal("Store on the returned Service is not the *store.Store passed to NewWithRemote")
	}
}
