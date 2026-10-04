package authority

import (
	"context"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
	"github.com/google/uuid"
	"os"
	"testing"
	"time"
)

func TestOrchestratorGrantExpiryRecoveryAuthority(t *testing.T) {
	for _, route := range []string{"agent.activate", "agent.switch-role"} {
		t.Run(route, func(t *testing.T) {
			serviceSigner, err := controlplane.GenerateSigner()
			if err != nil {
				t.Fatal(err)
			}
			databaseURL := os.Getenv("AGENT_COMMS_TEST_POSTGRES_URL")
			if databaseURL == "" {
				t.Skip("AGENT_COMMS_TEST_POSTGRES_URL is not configured")
			}
			engine, err := Open(context.Background(), Config{DatabaseURL: databaseURL}, serviceSigner)
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			now := time.Now().UTC().Truncate(time.Microsecond)
			engine.now = func() time.Time { return now }
			projectID := "grant-expiry-" + uuid.NewString()
			if err := engine.CreateProject(context.Background(), projectID, "owner"); err != nil {
				t.Fatal(err)
			}
			owner, err := controlplane.GenerateSigner()
			if err != nil {
				t.Fatal(err)
			}
			target, err := controlplane.GenerateSigner()
			if err != nil {
				t.Fatal(err)
			}
			mutate := func(actor string, signer *controlplane.Signer, typ, id string, payload any) error {
				raw, err := model.EncodePayload(typ, payload)
				if err != nil {
					return err
				}
				cmd := controlplane.Command{ProjectID: projectID, Actor: actor, Type: typ, EntityID: id, Payload: raw, IdempotencyKey: uuid.NewString(), IssuedAt: now}
				if typ == "agent.register" {
					cmd.PublicKey = signer.PublicKey()
				}
				if err := cmd.Sign(signer.PrivateKey()); err != nil {
					return err
				}
				_, _, err = engine.Mutate(context.Background(), cmd)
				return err
			}
			must := func(actor string, signer *controlplane.Signer, typ, id string, payload any) {
				t.Helper()
				if err := mutate(actor, signer, typ, id, payload); err != nil {
					t.Fatalf("%s: %v", typ, err)
				}
			}
			for _, who := range []struct {
				id     string
				signer *controlplane.Signer
			}{{"owner", owner}, {"target", target}} {
				must(who.id, who.signer, "agent.register", who.id, model.AgentRegistered{PublicKey: who.signer.PublicKey(), PrincipalType: model.PrincipalHuman, DisplayName: who.id})
				role := model.Role("MEMBER")
				if who.id == "owner" {
					role = model.RoleOwner
				}
				must("owner", owner, "agent.activate", who.id, model.AgentActivated{Role: role, Capabilities: []string{"*"}, Scopes: []string{"*"}})
			}
			id, action := protocol.OrchestratorGrantApprovalID("target"), protocol.OrchestratorGrantApprovalAction("target")
			expiry := now.Add(time.Minute)
			must("owner", owner, "approval.request", id, model.ApprovalRequested{Tier: "HUMAN", Action: action, ExpiresAt: &expiry, Reason: "initial"})
			must("owner", owner, "approval.approve", id, model.ApprovalResponse{})
			actor, signer := "owner", owner
			var grant any = model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"*"}}
			if route == "agent.switch-role" {
				actor, signer, grant = "target", target, model.AgentRoleSwitched{Role: model.RoleOrchestrator}
			}
			now = expiry
			_, beforeMeta, err := engine.State(context.Background(), projectID)
			if err != nil {
				t.Fatal(err)
			}
			if err := mutate(actor, signer, route, "target", grant); err == nil {
				t.Fatal("expired approval granted orchestrator")
			}
			after, afterMeta, err := engine.State(context.Background(), projectID)
			if err != nil {
				t.Fatal(err)
			}
			if afterMeta.ServerSequence != beforeMeta.ServerSequence || after.Agents["target"].Role == model.RoleOrchestrator || after.Approvals[id].Status != "APPROVED" {
				t.Fatal("rejected grant mutated history")
			}
			future := now.Add(time.Hour)
			must("target", target, "approval.request", id, model.ApprovalRequested{Tier: "HUMAN", Action: action, ExpiresAt: &future, Reason: "fresh"})
			fresh, _, err := engine.State(context.Background(), projectID)
			if err != nil {
				t.Fatal(err)
			}
			if fresh.Approvals[id].Status != "PENDING" || fresh.Approvals[id].Approver != "" || fresh.Approvals[id].Requester != "target" {
				t.Fatal("replacement inherited authorization")
			}
			if err := mutate(actor, signer, route, "target", grant); err == nil {
				t.Fatal("fresh pending approval granted orchestrator")
			}
			must("owner", owner, "approval.approve", id, model.ApprovalResponse{})
			must(actor, signer, route, "target", grant)
			final, _, err := engine.State(context.Background(), projectID)
			if err != nil {
				t.Fatal(err)
			}
			if final.Agents["target"].Role != model.RoleOrchestrator || final.Approvals[id].Status != "CONSUMED" {
				t.Fatal("fresh approval not consumed")
			}
			if err := mutate(actor, signer, route, "target", grant); err == nil {
				t.Fatal("consumed approval reused")
			}
		})
	}
}
