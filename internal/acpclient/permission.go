package acpclient

import acpsdk "github.com/coder/acp-go-sdk"

// Decision classifies how a ToolKind-tagged permission request from an ACP
// agent should be resolved under this project's hybrid approval model: cheap,
// low-risk tool kinds are approved automatically; edit-shaped kinds respect
// whatever edit-acceptance mode the invocation was configured with; anything
// that deletes, executes, or reaches outside the sandbox is routed through
// Agent Comms' own governance/approval flow rather than decided locally.
type Decision int

const (
	DecisionAutoApprove Decision = iota
	DecisionModeGated
	DecisionGoverned
)

// Classify maps an ACP ToolKind to the Decision that governs it. Unknown or
// future ToolKind values fall through to DecisionGoverned — this policy errs
// toward asking, never toward silently trusting a tool kind it doesn't
// recognize.
func Classify(kind acpsdk.ToolKind) Decision {
	switch kind {
	case acpsdk.ToolKindRead, acpsdk.ToolKindSearch, acpsdk.ToolKindThink, acpsdk.ToolKindSwitchMode:
		return DecisionAutoApprove
	case acpsdk.ToolKindEdit, acpsdk.ToolKindMove:
		return DecisionModeGated
	default:
		return DecisionGoverned
	}
}
