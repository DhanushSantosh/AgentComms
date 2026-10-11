package interactiveserve

import (
	"testing"
	"time"
)

// newTestTarget wires a delivery target around w the way Serve does, with
// screen standing in for the child's rendered output.
func newTestTarget(w writeFlusher, screen *terminalScreen) *ptyTarget {
	return &ptyTarget{input: w, screen: screen, gate: newInputGate(w)}
}

// deliverToPty runs one delivery against w. idleTO/echoTO are the real
// package constants in production; tests pass short overrides rather than
// waiting out the real, deliberately generous values.
func deliverToPty(w writeFlusher, screen *terminalScreen, message string, idleTO, echoTO time.Duration) error {
	_, err := newTestTarget(w, screen).deliver(message, "", idleTO, echoTO)
	return err
}

// screenOf renders raw terminal output on a fresh cols x rows screen.
func screenOf(t *testing.T, cols, rows int, raw string) screenSnapshot {
	t.Helper()
	screen := newTerminalScreen(cols, rows)
	t.Cleanup(screen.Close)
	if _, err := screen.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	return screen.snapshot()
}
