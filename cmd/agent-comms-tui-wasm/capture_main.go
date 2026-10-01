//go:build !js

package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/tui"
	"github.com/charmbracelet/colorprofile"
)

// Native entrypoint for recording the same isolated demo as the website.
// It uses the real TUI and governed seed transitions, never a live project's
// credentials, inbox, or private content. The js/wasm entrypoint is unchanged.
func main() {
	svc, err := bootstrapDemoService()
	if err == nil {
		err = seedDemoProject(svc)
	}
	if err == nil {
		err = tui.Run(svc, demoOwner, os.Stdin, os.Stdout, tea.WithColorProfile(colorprofile.TrueColor))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-comms demo:", err)
		os.Exit(1)
	}
}
