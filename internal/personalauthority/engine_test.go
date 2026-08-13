package personalauthority

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/google/uuid"
)

func TestTransactionalAuthorityRejectsConcurrentClaims(t *testing.T) {
	serviceSigner, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := Open(t.TempDir()+"/authority.db", serviceSigner)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	const projectID = "personal-project"
	if err = engine.CreateProject(context.Background(), projectID, "owner"); err != nil {
		t.Fatal(err)
	}
	owner, _ := controlplane.GenerateSigner()
	alpha, _ := controlplane.GenerateSigner()
	beta, _ := controlplane.GenerateSigner()
	mutate := func(actor string, signer *controlplane.Signer, eventType, entityID string, payload any, key string) (controlplane.Event, controlplane.Receipt, error) {
		raw, encodeErr := model.EncodePayload(eventType, payload)
		if encodeErr != nil {
			return controlplane.Event{}, controlplane.Receipt{}, encodeErr
		}
		command := controlplane.Command{
			ProjectID: projectID, Actor: actor, Type: eventType, EntityID: entityID,
			Payload: raw, IdempotencyKey: key, IssuedAt: time.Now().UTC(),
		}
		if eventType == "agent.register" {
			command.PublicKey = signer.PublicKey()
		}
		if signErr := command.Sign(signer.PrivateKey()); signErr != nil {
			return controlplane.Event{}, controlplane.Receipt{}, signErr
		}
		return engine.Mutate(context.Background(), command)
	}
	register := func(id string, signer *controlplane.Signer, principal model.PrincipalType) {
		t.Helper()
		_, _, registerErr := mutate(id, signer, "agent.register", id, model.AgentRegistered{
			PublicKey: signer.PublicKey(), PrincipalType: principal, DisplayName: id,
		}, uuid.NewString())
		if registerErr != nil {
			t.Fatal(registerErr)
		}
	}
	register("owner", owner, model.PrincipalHuman)
	if _, _, err = mutate("owner", owner, "agent.activate", "owner",
		model.AgentActivated{Role: model.RoleOwner, Capabilities: []string{"*"}, Scopes: []string{"*"}}, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	register("alpha", alpha, model.PrincipalAgent)
	register("beta", beta, model.PrincipalAgent)
	for _, agentID := range []string{"alpha", "beta"} {
		if _, _, err = mutate("owner", owner, "agent.activate", agentID,
			model.AgentActivated{Role: model.Role("MEMBER"), Scopes: []string{"src"}}, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
	}
	createPayload, err := model.EncodePayload("task.create", model.TaskCreated{
		Title: "Exclusive", Repository: "local", Branch: "feature", Resources: []string{"src/exclusive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	createCommand := controlplane.Command{
		ProjectID: projectID, Actor: "owner", Type: "task.create", EntityID: "exclusive",
		Payload: createPayload, IdempotencyKey: "create-exclusive", IssuedAt: time.Now().UTC(),
	}
	if err = createCommand.Sign(owner.PrivateKey()); err != nil {
		t.Fatal(err)
	}
	createCommandEvent, createReceipt, err := engine.Mutate(context.Background(), createCommand)
	if err != nil {
		t.Fatal(err)
	}
	replayedEvent, replayedReceipt, err := engine.Mutate(context.Background(), createCommand)
	if err != nil {
		t.Fatal(err)
	}
	if replayedEvent.ID != createCommandEvent.ID || replayedReceipt.Signature != createReceipt.Signature {
		t.Fatal("idempotent replay returned a different event or receipt")
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var writers sync.WaitGroup
	for agentID, signer := range map[string]*controlplane.Signer{"alpha": alpha, "beta": beta} {
		writers.Add(1)
		go func(agentID string, signer *controlplane.Signer) {
			defer writers.Done()
			<-start
			_, _, claimErr := mutate(agentID, signer, "task.claim", "exclusive", model.TaskClaimed{}, uuid.NewString())
			results <- claimErr
		}(agentID, signer)
	}
	close(start)
	writers.Wait()
	close(results)
	successes := 0
	for claimErr := range results {
		if claimErr == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent claims=%d, want 1", successes)
	}
	state, metadata, err := engine.State(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Tasks["exclusive"].Owner == "" {
		t.Fatal("exclusive task has no owner")
	}
	if metadata.Consistency != "PERSONAL_AUTHORITATIVE" || metadata.Connectivity != "LOCAL" {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
}
