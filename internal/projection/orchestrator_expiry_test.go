package projection

import (
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
)

func TestHistoricalExpiredOrchestratorGrantReplayAndFreshRequest(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	past := now.Add(-time.Second)
	for _, route := range []string{"agent.activate", "agent.switch-role"} {
		t.Run(route, func(t *testing.T) {
			id := protocol.OrchestratorGrantApprovalID("target")
			action := protocol.OrchestratorGrantApprovalAction("target")
			state := model.State{Agents: map[string]model.Agent{"target": {ID: "target", Role: model.Role("MEMBER")}}, Approvals: map[string]model.Approval{id: {ID: id, Tier: "HUMAN", Action: action, Status: "APPROVED", ExpiresAt: &past}}}
			var payload any = model.AgentActivated{Role: model.RoleOrchestrator}
			if route == "agent.switch-role" {
				payload = model.AgentRoleSwitched{Role: model.RoleOrchestrator}
			}
			data, err := model.EncodePayload(route, payload)
			if err != nil {
				t.Fatal(err)
			}
			if err := ApplyEvent(&state, model.Event{ID: "historical-grant", Type: route, EntityID: "target", Actor: "target", Time: now, Data: data}); err != nil {
				t.Fatal(err)
			}
			if state.Agents["target"].Role != model.RoleOrchestrator || state.Approvals[id].Status != "CONSUMED" {
				t.Fatal("historical grant replay changed")
			}
			data, err = model.EncodePayload("approval.request", model.ApprovalRequested{Tier: "HUMAN", Action: action, Reason: "fresh human decision"})
			if err != nil {
				t.Fatal(err)
			}
			if err := ApplyEvent(&state, model.Event{ID: "new-request", Type: "approval.request", EntityID: id, Actor: "new-requester", Time: now.Add(time.Second), Data: data}); err != nil {
				t.Fatal(err)
			}
			approval := state.Approvals[id]
			if approval.Status != "PENDING" || approval.Requester != "new-requester" || approval.ExpiresAt != nil {
				t.Fatalf("stale approval inherited: %+v", approval)
			}
		})
	}
}
