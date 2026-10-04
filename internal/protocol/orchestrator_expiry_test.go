package protocol

import (
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestOrchestratorGrantExpiryBothRoutes(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Second), now.Add(time.Second)
	for _, route := range []string{"agent.activate", "agent.switch-role"} {
		for _, tc := range []struct {
			name   string
			expiry *time.Time
			allow  bool
		}{
			{"past", &past, false}, {"equal", &now, false}, {"future", &future, true}, {"legacy", nil, true},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				st := switchRoleState()
				id := "human-member"
				st.Approvals = map[string]model.Approval{OrchestratorGrantApprovalID(id): {Tier: "HUMAN", Action: OrchestratorGrantApprovalAction(id), Status: "APPROVED", ExpiresAt: tc.expiry}}
				actor := id
				var payload any = model.AgentRoleSwitched{Role: model.RoleOrchestrator}
				if route == "agent.activate" {
					actor = "owner"
					payload = model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"*"}}
				}
				_, err := ValidateTransition(st, actor, route, id, payload, now)
				if (err == nil) != tc.allow {
					t.Fatalf("allow=%v, error=%v", tc.allow, err)
				}
			})
		}
	}
}

func TestOrchestratorGrantExpiredRequestRecovery(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Second), now.Add(time.Second)
	id, action := OrchestratorGrantApprovalID("target"), OrchestratorGrantApprovalAction("target")
	for _, tc := range []struct {
		name, status, tier, action, id string
		expiry                         *time.Time
		allow                          bool
	}{
		{"expired pending", "PENDING", "HUMAN", action, id, &past, true},
		{"expired approved", "APPROVED", "HUMAN", action, id, &past, true},
		{"equal approved", "APPROVED", "HUMAN", action, id, &now, true},
		{"consumed", "CONSUMED", "HUMAN", action, id, nil, true},
		{"future", "APPROVED", "HUMAN", action, id, &future, false},
		{"legacy", "APPROVED", "HUMAN", action, id, nil, false},
		{"rejected", "REJECTED", "HUMAN", action, id, &past, false},
		{"wrong tier", "APPROVED", "ORCHESTRATOR", action, id, &past, false},
		{"wrong action", "APPROVED", "HUMAN", "other", id, &past, false},
		{"wrong id", "APPROVED", "HUMAN", action, "other", &past, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := model.State{Agents: map[string]model.Agent{"owner": humanAgent("owner")}, Approvals: map[string]model.Approval{tc.id: {Tier: tc.tier, Status: tc.status, Action: tc.action, ExpiresAt: tc.expiry}}}
			request := model.ApprovalRequested{Tier: "HUMAN", Action: action, Reason: "fresh decision", ExpiresAt: &future}
			_, err := ValidateTransition(st, "owner", "approval.request", tc.id, request, now)
			if (err == nil) != tc.allow {
				t.Fatalf("allow=%v, error=%v", tc.allow, err)
			}
			request.ExpiresAt = &now
			if _, err := ValidateTransition(st, "owner", "approval.request", tc.id, request, now); err == nil {
				t.Fatal("nonfuture replacement accepted")
			}
		})
	}
}
