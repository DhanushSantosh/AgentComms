//go:build !js

package doctor

import (
	"context"

	"github.com/DhanushSantosh/AgentComms/internal/interactiveserve"
)

// interactiveSocketProbe reports whether runtimeID's local PTY control
// socket is dialable, and whether the probe could be carried out at all.
// On every real platform it can, so probed is always true and the caller
// behaves exactly as it did when it called interactiveserve.Alive inline.
// The js/wasm variant (interactive_js.go) cannot dial anything and says so
// through probed, so that a finding is withheld for want of evidence rather
// than reported on a socket nobody looked at.
func interactiveSocketProbe(ctx context.Context, projectRoot, runtimeID string) (alive, probed bool) {
	return interactiveserve.Alive(ctx, projectRoot, runtimeID), true
}
