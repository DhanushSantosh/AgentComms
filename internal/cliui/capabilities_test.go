package cliui_test

import (
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
)

// TestNonInteractiveContextNeverGetsAGuessedWidth is the regression test
// for UX-04: CapabilitiesFor used to fall back to an 80-column default
// unconditionally, so redirected/piped output -- which DetectCapabilities
// never sets a real Width for, since it only probes term.GetSize on an
// actual TTY -- silently got the same column truncation an interactive
// 80-column terminal would, for no reason: there is no terminal to wrap
// against once output isn't going to one.
func TestNonInteractiveContextNeverGetsAGuessedWidth(t *testing.T) {
	caps := cliui.CapabilitiesFor(cliui.TerminalContext{Interactive: false, Width: 0})
	if caps.Width != 0 {
		t.Fatalf("non-interactive context got Width=%d, want 0 (never truncate)", caps.Width)
	}
}

// TestInteractiveContextStillFallsBackWhenSizeDetectionFails covers the
// case the 80-column fallback exists for: a real terminal session where
// term.GetSize itself failed (an old/unusual terminal), not a genuinely
// non-interactive destination.
func TestInteractiveContextStillFallsBackWhenSizeDetectionFails(t *testing.T) {
	caps := cliui.CapabilitiesFor(cliui.TerminalContext{Interactive: true, Width: 0})
	if caps.Width <= 0 {
		t.Fatalf("interactive context with undetected size got Width=%d, want a positive fallback", caps.Width)
	}
}

// TestInteractiveContextPrefersItsOwnDetectedWidth confirms the fallback
// only applies when Width is genuinely unknown, not whenever it's small.
func TestInteractiveContextPrefersItsOwnDetectedWidth(t *testing.T) {
	caps := cliui.CapabilitiesFor(cliui.TerminalContext{Interactive: true, Width: 40})
	if caps.Width != 40 {
		t.Fatalf("Width = %d, want the context's own detected 40", caps.Width)
	}
}
