package tui

import (
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
)

func TestOrchestratorGrantUIRejectsExpiredApproval(t *testing.T) {
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	for _, tc := range []struct {
		name   string
		expiry *time.Time
		allow  bool
	}{
		{"past", &past, false}, {"future", &future, true}, {"legacy", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := model.State{Approvals: map[string]model.Approval{protocol.OrchestratorGrantApprovalID("target"): {Tier: "HUMAN", Status: "APPROVED", Action: protocol.OrchestratorGrantApprovalAction("target"), ExpiresAt: tc.expiry}}}
			if got := hasApprovedOrchestratorGrant(state, "target"); got != tc.allow {
				t.Fatalf("eligible=%v want %v", got, tc.allow)
			}
		})
	}
}
