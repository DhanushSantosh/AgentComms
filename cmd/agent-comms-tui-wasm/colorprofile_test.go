package main

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/tui"
	"github.com/charmbracelet/colorprofile"
)

// syncBuf is a mutex-protected bytes.Buffer safe for concurrent
// writer(renderer goroutine)/reader(test goroutine) use -- the exact shape
// jsOutputWriter has (a plain io.Writer with no Fd() method, so it never
// satisfies term.File), used here as a host-side stand-in for it.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// cyanTrueColorSGR is the exact truecolor SGR escape lipgloss emits for
// internal/tui/model.go's colors(false).cyan (#56D6C9 = rgb(86,214,201)) --
// e.g. the "LIVE" label in commandRail. Its presence/absence in captured
// output is the test oracle for whether the color profile bubbletea
// negotiated actually let color through, or stripped it.
const cyanTrueColorSGR = "38;2;86;214;201"

// runTUIAndCapture bootstraps the same real demo service/model wasm_main.go
// uses, runs it through tui.Run with the given extra options, feeds it a
// real (non-zero) resize event followed by "q" to quit, and returns
// everything written to the program's output.
func runTUIAndCapture(t *testing.T, opts ...tea.ProgramOption) string {
	t.Helper()

	svc, err := bootstrapDemoService()
	if err != nil {
		t.Fatalf("bootstrapDemoService: %v", err)
	}
	if err := seedDemoProject(svc); err != nil {
		t.Fatalf("seedDemoProject: %v", err)
	}

	pr, pw := io.Pipe()
	out := &syncBuf{}

	runDone := make(chan error, 1)
	go func() {
		runDone <- tui.Run(svc, demoOwner, pr, out, opts...)
	}()

	go func() {
		_, _ = pw.Write(encodeWindowSizeEvent(100, 30))
		time.Sleep(150 * time.Millisecond) // let the resize actually repaint
		_, _ = pw.Write([]byte("q"))       // internal/tui/model.go: "q" -> tea.Quit
	}()

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("tui.Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for tui.Run to quit")
	}
	_ = pw.Close()

	return out.String()
}

// TestRunWithoutColorProfileStripsColor proves the bug the reviewer flagged:
// with no color-profile option (bubbletea's default auto-detection, exactly
// what wasm_main.go did before this fix), a jsOutputWriter-shaped io.Writer
// (no Fd(), so never a term.File) makes colorprofile.Detect return NoTTY
// unconditionally -- every ANSI color code the real product model emits gets
// stripped before it reaches the output writer.
func TestRunWithoutColorProfileStripsColor(t *testing.T) {
	// Explicit empty environment (not the default fallback to the real
	// os.Environ()) so this test's outcome doesn't depend on whatever
	// TERM/COLORTERM happens to be set wherever it runs -- isatty is false
	// regardless (syncBuf, like jsOutputWriter, has no Fd()), so detection
	// must land on NoTTY here no matter what.
	got := runTUIAndCapture(t, tea.WithEnvironment([]string{}))
	if strings.Contains(got, cyanTrueColorSGR) {
		t.Fatalf("expected NO truecolor SGR code with no explicit color profile (bubbletea's default detection should have stripped it against a non-tty writer), but found one in: %q", got)
	}
}

// TestRunWithColorProfileTrueColorSurvivesToOutput proves the fix: passing
// tea.WithColorProfile(colorprofile.TrueColor) into tui.Run (as wasm_main.go
// now does) makes the same real product model's color codes survive to the
// output writer, deterministically, with no dependency on TERM/COLORTERM or
// any other environment variable.
func TestRunWithColorProfileTrueColorSurvivesToOutput(t *testing.T) {
	// WithEnvironment([]string{}) (not nil -- tea.go falls back to the real
	// os.Environ() when p.environ is nil) forces a genuinely empty
	// environment regardless of whatever TERM/COLORTERM this test happens
	// to run under, so a pass here can only be explained by the explicit
	// WithColorProfile option, not by the host's own terminal.
	got := runTUIAndCapture(t, tea.WithColorProfile(colorprofile.TrueColor), tea.WithEnvironment([]string{}))
	if !strings.Contains(got, cyanTrueColorSGR) {
		t.Fatalf("expected the truecolor SGR code %q to survive with an explicit WithColorProfile(TrueColor), but it did not appear in: %q", cyanTrueColorSGR, got)
	}
}
