//go:build js && wasm

// This file is the actual GOOS=js GOARCH=wasm entrypoint. It is deliberately
// minimal: real I/O wiring to xterm.js (the in/out this program's TUI reads
// from and writes to) is a later task's job, not this one. os.Stdin/
// os.Stdout are the simplest possible placeholder here -- under GOOS=js
// neither is a real terminal, but tui.Run only needs an io.Reader/io.Writer,
// and this proves the entrypoint compiles and the demo project seeds
// correctly; a later task swaps these two lines for the real bridge.
package main

import (
	"fmt"
	"os"

	"github.com/DhanushSantosh/AgentComms/internal/tui"
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

	go func() {
		if runErr := tui.Run(svc, demoOwner, os.Stdin, os.Stdout); runErr != nil {
			fmt.Fprintln(os.Stderr, "agent-comms-tui-wasm: tui exited:", runErr)
		}
	}()

	// A WASM program's main returning ends the whole module -- block
	// forever so the TUI goroutine keeps running.
	select {}
}
