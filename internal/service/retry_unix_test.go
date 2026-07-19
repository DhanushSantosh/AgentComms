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
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestRemoteMutationReplaysSameCommandAfterLostResponse(t *testing.T) {
	root := t.TempDir()
	instance := New(root)
	credentials := identity.NewMemoryStore()
	instance.Store.SetCredentialStore(credentials)
	if err := instance.Store.Init("owner"); err != nil {
		t.Fatal(err)
	}
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
