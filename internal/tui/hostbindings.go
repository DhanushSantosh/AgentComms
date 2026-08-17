//go:build !js

package tui

import (
	"context"
	"fmt"

	"github.com/DhanushSantosh/AgentComms/internal/interactiveserve"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
)

// This file and hostbindings_js.go hold the handful of TUI behaviors that
// depend on a real host: dialing an interactive runtime's local PTY control
// socket (internal/interactiveserve, which needs a real OS process, a real
// terminal and a real unix socket) and reading a real project's lifecycle
// state off disk. This file is the real one and is what every ordinary
// build compiles; the js/wasm variant reports those capabilities as
// unavailable rather than pretending to have them. The functions here are
// deliberately verbatim extractions of logic that used to sit inline at
// their call sites, so no real build changes behavior by an inch.

// probePTYState renders the "Local PTY" runtime-detail line for a local
// interactive runtime by dialing its control socket.
func probePTYState(ctx context.Context, projectRoot, runtimeID string) string {
	alive, busy := interactiveserve.Probe(ctx, projectRoot, runtimeID)
	switch {
	case alive && busy:
		return "live · busy"
	case alive:
		return "live · idle"
	default:
		return "not dialable"
	}
}

// ptySnapshot returns a snapshot of a live runtime's PTY output buffer.
func ptySnapshot(ctx context.Context, projectRoot, runtimeID string) (string, error) {
	return interactiveserve.Snapshot(ctx, projectRoot, runtimeID)
}

// lifecycleCompatibility renders the "Compatibility" line of the audit
// view's project-lifecycle summary from an inspected plan.
func lifecycleCompatibility(plan projectlifecycle.Plan) string {
	if len(plan.Actions) > 0 {
		return fmt.Sprintf("%d UPGRADE ACTION(S)", len(plan.Actions))
	}
	return "CURRENT"
}
