//go:build js

package daemon

import (
	"context"
	"errors"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// notifyLocalInteractive always fails in a js/wasm build. Delivering to an
// INTERACTIVE runtime means typing into a live terminal owned by a real OS
// process (internal/interactiveserve, on github.com/creack/pty); a browser
// sandbox has neither, so there is no js implementation to write. Failing
// loudly at the delivery attempt is the honest answer -- the dispatcher
// records a failed delivery, exactly as it would for an unreachable
// connector, rather than reporting PTY evidence for keystrokes nobody sent.
func notifyLocalInteractive(
	ctx context.Context,
	projectRoot, runtimeID, agentID, invocationID, requestedBy string,
) ([]model.DeliveryEvidence, error) {
	_, _, _ = ctx, projectRoot, runtimeID
	_, _ = agentID, invocationID
	_ = requestedBy
	return nil, errors.New(
		"interactive PTY delivery is unavailable in js/wasm builds: there is no local terminal to drive")
}
