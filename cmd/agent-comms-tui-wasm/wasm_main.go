//go:build js && wasm

// This file is the actual GOOS=js GOARCH=wasm entrypoint. Terminal I/O is
// bridged to xterm.js in the browser via jsbridge.go's syscall/js exports
// (agentCommsTUIWrite/agentCommsTUIResize) and its window.agentCommsTUIOnOutput
// output callback -- see jsbridge.go for the full contract.
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/tui"
	"github.com/charmbracelet/colorprofile"
)

// defaultCols/defaultRows match internal/tui/model.go's own Model defaults
// (width: 100, height: 30) so the TUI has a sane initial size to render at
// before xterm.js's real onResize fires with the browser's actual terminal
// dimensions.
const (
	defaultCols = 100
	defaultRows = 30
)

func main() {
	svc, err := bootstrapDemoService()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-comms-tui-wasm: bootstrap failed:", err)
		select {}
	}
	if err = seedDemoProject(svc); err != nil {
		fmt.Fprintln(os.Stderr, "agent-comms-tui-wasm: seed failed:", err)
		select {}
	}

	input := newJSInputBuffer()
	output := jsOutputWriter{}
	registerJSBridge(input)

	// No real terminal exists to auto-detect a size from under GOOS=js --
	// seed a synthetic resize event up front so the TUI has a size to render
	// at even before xterm.js's onResize fires for the first time.
	input.write(encodeWindowSizeEvent(defaultCols, defaultRows))

	// bubbletea's default color-profile auto-detection
	// (colorprofile.Detect(p.output, p.environ)) can never see a real
	// terminal here: jsOutputWriter has no Fd() method so it never satisfies
	// term.File, which forces isatty=false regardless of any TERM/COLORTERM
	// environment variables -- detection falls back to NoTTY and every ANSI
	// color code lipgloss emits gets stripped before it reaches xterm.js.
	// Setting the profile explicitly sidesteps that detection entirely:
	// xterm.js always supports truecolor, so this is simply always correct
	// for this entrypoint (verified in
	// cmd/agent-comms-tui-wasm/colorprofile_test.go, which also proves the
	// alternative -- setting TERM/COLORTERM alone via a JS-side environment
	// override -- is NOT sufficient, since isatty is never true no matter
	// what environment variables are set).
	colorOpt := tea.WithColorProfile(colorprofile.TrueColor)

	go func() {
		if runErr := tui.Run(svc, demoOwner, input, output, colorOpt); runErr != nil {
			fmt.Fprintln(os.Stderr, "agent-comms-tui-wasm: tui exited:", runErr)
		}
	}()

	// A WASM program's main returning ends the whole module -- block
	// forever so the TUI goroutine keeps running.
	select {}
}
