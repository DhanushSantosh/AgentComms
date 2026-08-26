//go:build js

package tui

import (
	"context"
	"errors"

	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
)

// js/wasm variants of the TUI's host-dependent behaviors (see
// hostbindings.go). Two capabilities are genuinely, permanently absent in a
// browser sandbox rather than merely unimplemented:
//
//   - The Direct Agent Relay's live-session binding. It works by dialing an
//     interactive runtime's local PTY control socket, driven by a real OS
//     process attached to a real terminal (internal/interactiveserve, on
//     github.com/creack/pty). There is no OS process and no PTY inside a
//     browser tab, so there is nothing to dial and no js equivalent to
//     write. The Runtimes view stays present and navigable and simply says
//     so on the affected line.
//   - Project lifecycle inspection, which reads schema versions out of real
//     on-disk SQLite databases (see internal/projectlifecycle's own js
//     variant). Compatibility is reported as unknown here rather than
//     CURRENT: the js Inspect returns no upgrade actions because it computed
//     none, which is not the same claim as "this project is up to date."
//
// Every real build compiles hostbindings.go instead and is untouched by any
// of this.

const ptyUnavailableState = "unavailable in this build"

// probePTYState reports that the local PTY socket cannot be dialed from a
// WASM build, instead of returning "not dialable" -- which would be a claim
// about a socket nobody actually probed.
func probePTYState(ctx context.Context, projectRoot, runtimeID string) string {
	_, _, _ = ctx, projectRoot, runtimeID
	return ptyUnavailableState
}

// ptySnapshot always fails: there is no live PTY to snapshot. The Runtimes
// detail pane only asks for a snapshot when probePTYState reported a live
// session, which it never does here, so this is a belt-and-braces answer for
// any future caller rather than a path the demo walks.
func ptySnapshot(ctx context.Context, projectRoot, runtimeID string) (string, error) {
	_, _, _ = ctx, projectRoot, runtimeID
	return "", errors.New("live PTY relay is unavailable in this build: a browser sandbox has no terminal to bind")
}

// lifecycleCompatibility reports the lifecycle plan as un-inspectable rather
// than reading an empty action list as a clean bill of health.
func lifecycleCompatibility(plan projectlifecycle.Plan) string {
	_ = plan
	return "UNAVAILABLE IN THIS BUILD"
}
