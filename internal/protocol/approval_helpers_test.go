package protocol

import (
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// hasApproval reports whether an eligible approval exists for action; a
// test-only view of eligibleActionApprovalID.
func hasApproval(st model.State, action string, now time.Time) bool {
	id, _ := eligibleActionApprovalID(st, action, now)
	return id != ""
}
