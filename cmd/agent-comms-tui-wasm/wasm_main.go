//go:build js && wasm

// This file is the actual GOOS=js GOARCH=wasm entrypoint. Terminal I/O is
// bridged to xterm.js in the browser via jsbridge.go's syscall/js exports
// (agentCommsTUIWrite/agentCommsTUIResize) and its window.agentCommsTUIOnOutput
// output callback -- see jsbridge.go for the full contract.
package main

import (
	"fmt"
	"os"

	"github.com/DhanushSantosh/AgentComms/internal/tui"
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

	go func() {
		if runErr := tui.Run(svc, demoOwner, input, output); runErr != nil {
			fmt.Fprintln(os.Stderr, "agent-comms-tui-wasm: tui exited:", runErr)
		}
	}()

	// A WASM program's main returning ends the whole module -- block
	// forever so the TUI goroutine keeps running.
	select {}
}
