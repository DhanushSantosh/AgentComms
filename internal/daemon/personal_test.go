package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/localcache"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/personalauthority"
)

func TestPersonalDaemonCommitsAndServesAuthoritativeLocalState(t *testing.T) {
	serviceSigner, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	authority, err := personalauthority.Open(filepath.Join(t.TempDir(), "authority.db"), serviceSigner)
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	if err = authority.CreateProject(context.Background(), "project", "owner"); err != nil {
		t.Fatal(err)
	}
	cache, err := localcache.Open(filepath.Join(t.TempDir(), "projection.db"), serviceSigner.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	instance, err := New(cache, authority)
	if err != nil {
		t.Fatal(err)
	}
	instance.SetPersonalMode(true)
	owner, _ := controlplane.GenerateSigner()
	command := signedCommand(t, owner, "project", "owner", "agent.register", "owner",
		model.AgentRegistered{PublicKey: owner.PublicKey(), PrincipalType: model.PrincipalHuman, DisplayName: "Owner"}, "register-owner")
	response := requestCommand(t, instance.Handler(), command)
	if response.Metadata.Consistency != "PERSONAL_AUTHORITATIVE" ||
		response.Metadata.Connectivity != "LOCAL" || response.Metadata.Receipt == nil {
		t.Fatalf("unexpected personal mutation metadata: %+v", response.Metadata)
	}
	command = signedCommand(t, owner, "project", "owner", "agent.activate", "owner",
		model.AgentActivated{Role: model.RoleOwner, Capabilities: []string{"*"}, Scopes: []string{"*"}}, "activate-owner")
	_ = requestCommand(t, instance.Handler(), command)
	start := make(chan struct{})
	results := make(chan int, 2)
	var writers sync.WaitGroup
	for agentID, signer := range map[string]*controlplane.Signer{
		"alpha": mustSigner(t), "beta": mustSigner(t),
	} {
		command = signedCommand(t, signer, "project", agentID, "agent.register", agentID,
			model.AgentRegistered{PublicKey: signer.PublicKey(), PrincipalType: model.PrincipalAgent, DisplayName: agentID}, "register-"+agentID)
		writers.Add(1)
		go func(command controlplane.Command) {
			defer writers.Done()
			<-start
			body, _ := json.Marshal(command)
			request := httptest.NewRequest(http.MethodPost, "/v1/projects/project/commands", bytes.NewReader(body))
			request.SetPathValue("project", "project")
			recorder := httptest.NewRecorder()
			instance.Handler().ServeHTTP(recorder, request)
			results <- recorder.Code
		}(command)
	}
	close(start)
	writers.Wait()
	close(results)
	for status := range results {
		if status != http.StatusOK {
			t.Fatalf("concurrent personal command status=%d", status)
		}
	}

	stateRequest := httptest.NewRequest(http.MethodGet, "/v1/projects/project/state", nil)
	stateRequest.SetPathValue("project", "project")
	stateRecorder := httptest.NewRecorder()
	instance.Handler().ServeHTTP(stateRecorder, stateRequest)
	if stateRecorder.Code != http.StatusOK {
		t.Fatalf("state status=%d body=%s", stateRecorder.Code, stateRecorder.Body.String())
	}
	var stateResponse struct {
		State    model.State                 `json:"state"`
		Metadata controlplane.ResultMetadata `json:"metadata"`
	}
	if err = json.Unmarshal(stateRecorder.Body.Bytes(), &stateResponse); err != nil {
		t.Fatal(err)
	}
	if stateResponse.State.Agents["owner"].Role != model.RoleOwner ||
		stateResponse.State.Integrity.EventCount != 4 ||
		stateResponse.Metadata.Consistency != "PERSONAL_AUTHORITATIVE" {
		t.Fatalf("unexpected personal state response: %+v", stateResponse)
	}
}

func mustSigner(t *testing.T) *controlplane.Signer {
	t.Helper()
	signer, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestDaemonShutdownEndpointSignalsLifecycle(t *testing.T) {
	instance := &Daemon{}
	shutdown := make(chan struct{})
	instance.SetShutdown(func() { close(shutdown) })
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/shutdown", bytes.NewReader([]byte(`{}`)))
	recorder := httptest.NewRecorder()
	instance.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("shutdown status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	select {
	case <-shutdown:
	case <-time.After(time.Second):
		t.Fatal("shutdown callback was not invoked")
	}
}

func signedCommand(t *testing.T, signer *controlplane.Signer, projectID, actor, eventType, entityID string, payload any, key string) controlplane.Command {
	t.Helper()
	raw, err := model.EncodePayload(eventType, payload)
	if err != nil {
		t.Fatal(err)
	}
	command := controlplane.Command{
		ProjectID: projectID, Actor: actor, Type: eventType, EntityID: entityID,
		Payload: raw, IdempotencyKey: key, IssuedAt: time.Now().UTC(),
	}
	if eventType == "agent.register" {
		command.PublicKey = signer.PublicKey()
	}
	if err = command.Sign(signer.PrivateKey()); err != nil {
		t.Fatal(err)
	}
	return command
}

func requestCommand(t *testing.T, handler http.Handler, command controlplane.Command) struct {
	Event    controlplane.Event          `json:"event"`
	Metadata controlplane.ResultMetadata `json:"metadata"`
} {
	t.Helper()
	body, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/projects/project/commands", bytes.NewReader(body))
	request.SetPathValue("project", "project")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("command status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Event    controlplane.Event          `json:"event"`
		Metadata controlplane.ResultMetadata `json:"metadata"`
	}
	if err = json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}
