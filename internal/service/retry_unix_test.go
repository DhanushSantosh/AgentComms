//go:build !windows

package service

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/daemonclient"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/runtimeinit"
)

func TestRemoteMutationReplaysSameCommandAfterLostResponse(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(root, "credentials"))
	if _, err := runtimeinit.Initialize(t.Context(), runtimeinit.Config{
		ProjectRoot: root, Owner: "owner", Mode: "personal",
	}); err != nil {
		t.Fatal(err)
	}
	instance := New(root)
	config, err := instance.Store.Config()
	if err != nil {
		t.Fatal(err)
	}
	socketDir, err := os.MkdirTemp("/tmp", "ac-retry-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	endpoint := filepath.Join(socketDir, "daemon.sock")
	badListener, err := net.Listen("unix", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	var badCommand controlplane.Command
	badReceived := make(chan struct{})
	var receiveOnce sync.Once
	badServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&badCommand); err != nil {
			t.Error(err)
			return
		}
		receiveOnce.Do(func() { close(badReceived) })
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("response writer cannot simulate a lost response")
			return
		}
		connection, _, hijackErr := hijacker.Hijack()
		if hijackErr != nil {
			t.Error(hijackErr)
			return
		}
		_ = connection.Close()
	})}
	go func() { _ = badServer.Serve(badListener) }()
	client, err := daemonclient.New(endpoint, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	instance.remote = client
	var goodCommand controlplane.Command
	var goodServer *http.Server
	instance.SetRemoteRecovery(func() error {
		_ = badServer.Close()
		listener, listenErr := net.Listen("unix", endpoint)
		if listenErr != nil {
			return listenErr
		}
		goodServer = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if decodeErr := json.NewDecoder(request.Body).Decode(&goodCommand); decodeErr != nil {
				t.Error(decodeErr)
				http.Error(w, decodeErr.Error(), http.StatusBadRequest)
				return
			}
			event := controlplane.Event{
				ProjectID: goodCommand.ProjectID, Sequence: 3, ID: "evt-replayed",
				Time: time.Now().UTC(), Actor: goodCommand.Actor, Type: goodCommand.Type,
				EntityID: goodCommand.EntityID, Payload: goodCommand.Payload,
				ActorIntentHash: "intent", IdempotencyKey: goodCommand.IdempotencyKey,
			}
			event.Hash, _ = controlplane.HashEvent(event)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"event": event,
				"metadata": controlplane.ResultMetadata{
					Consistency: "PERSONAL_AUTHORITATIVE", ServerSequence: 3,
					CacheSequence: 3, Connectivity: "LOCAL",
				},
			})
		})}
		go func() { _ = goodServer.Serve(listener) }()
		return nil
	})
	t.Cleanup(func() {
		_ = badServer.Close()
		if goodServer != nil {
			_ = goodServer.Close()
		}
	})
	_, err = instance.Execute("owner", "task.create", "task-retry", model.TaskCreated{
		Title: "Retry", Repository: "local", Branch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-badReceived:
	case <-time.After(time.Second):
		t.Fatal("first command was not received")
	}
	if badCommand.IdempotencyKey == "" || badCommand.IdempotencyKey != goodCommand.IdempotencyKey ||
		badCommand.Signature != goodCommand.Signature {
		t.Fatalf("retry changed signed intent: first=%+v second=%+v", badCommand, goodCommand)
	}
	if config.ProjectID != goodCommand.ProjectID {
		t.Fatalf("retry project=%s, want %s", goodCommand.ProjectID, config.ProjectID)
	}
}

// TestStateRecoversFromMissingDaemonSocket is a regression test for a real
// bug: State() used to make exactly one remote call with no retry and no
// recovery, unlike executeRemoteWithCredential -- so a single transient
// "local daemon is unavailable: dial unix ... no such file or directory"
// (e.g. the daemon's socket briefly gone, matching the reported symptom of
// runtime interactive-serve's heartbeat loop tearing down an entire live
// session on the very next tick) permanently failed the call instead of
// recovering. State() and Command() now share retryOnDaemonOffline, so this
// exercises the read path the same way
// TestRemoteMutationReplaysSameCommandAfterLostResponse already exercises
// the write path: point at a socket that doesn't exist yet (simulating the
// exact "no such file or directory" symptom), and confirm recoverRemote
// creating it mid-retry is enough for State() to succeed.
func TestStateRecoversFromMissingDaemonSocket(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(root, "credentials"))
	if _, err := runtimeinit.Initialize(t.Context(), runtimeinit.Config{
		ProjectRoot: root, Owner: "owner", Mode: "personal",
	}); err != nil {
		t.Fatal(err)
	}
	instance := New(root)
	socketDir, err := os.MkdirTemp("/tmp", "ac-retry-state-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	// Deliberately does not exist yet -- dialing it produces the exact
	// "no such file or directory" symptom from the bug report, not a
	// connection refusal.
	endpoint := filepath.Join(socketDir, "daemon.sock")
	client, err := daemonclient.New(endpoint, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	instance.remote = client
	var server *http.Server
	recovered := false
	instance.SetRemoteRecovery(func() error {
		recovered = true
		listener, listenErr := net.Listen("unix", endpoint)
		if listenErr != nil {
			return listenErr
		}
		server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state": model.State{},
				"metadata": controlplane.ResultMetadata{
					Consistency: "PERSONAL_AUTHORITATIVE", Connectivity: "LOCAL",
				},
			})
		})}
		go func() { _ = server.Serve(listener) }()
		return nil
	})
	t.Cleanup(func() {
		if server != nil {
			_ = server.Close()
		}
	})
	if _, err := instance.State(); err != nil {
		t.Fatalf("State() should have recovered after the socket appeared, got: %v", err)
	}
	if !recovered {
		t.Fatal("expected recoverRemote to be called")
	}
}
