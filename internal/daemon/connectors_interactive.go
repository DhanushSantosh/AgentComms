//go:build !js

package daemon

import (
	"context"

	"github.com/DhanushSantosh/AgentComms/internal/interactiveserve"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// notifyLocalInteractive delivers an invocation to a local interactive
// runtime by driving its live PTY through internal/interactiveserve, and
// returns the delivery evidence that drive produced. Extracted verbatim from
// SetLocalInteractive's dispatch closure so the js/wasm build can report the
// capability as absent (connectors_interactive_js.go) without
// internal/interactiveserve -- and github.com/creack/pty underneath it --
// entering the graph of every package that imports internal/daemon.
func notifyLocalInteractive(
	ctx context.Context,
	projectRoot, runtimeID, agentID, invocationID, requestedBy string,
) ([]model.DeliveryEvidence, error) {
	receipt, err := interactiveserve.NotifyInvocationWithEvidence(
		ctx, projectRoot, runtimeID, agentID, invocationID, requestedBy,
	)
	if err != nil {
		return nil, err
	}
	return []model.DeliveryEvidence{
		{Stage: "PTY_TEXT_ECHOED", At: receipt.TextEchoedAt},
		{Stage: "PTY_ENTER_SENT", At: receipt.EnterSentAt},
	}, nil
}
