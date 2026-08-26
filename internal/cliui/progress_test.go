package cliui_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
)

func TestProgressIsTTYOnlyAndAlwaysFinishesOnItsOwnLine(t *testing.T) {
	var terminal bytes.Buffer
	progress := cliui.NewProgress(&terminal, cliui.ModeHuman, cliui.Capabilities{Interactive: true, Unicode: true}, false)
	if err := progress.Start("Applying update\x1b[31m"); err != nil {
		t.Fatal(err)
	}
	if err := progress.Stop(true, "Update installed"); err != nil {
		t.Fatal(err)
	}
	if got := terminal.String(); !strings.Contains(got, "Applying update") || !strings.Contains(got, "Update installed") || !strings.HasSuffix(got, "\n") || strings.Contains(got, "\x1b[31m") {
		t.Fatalf("unexpected terminal progress lifecycle: %q", got)
	}

	var redirected bytes.Buffer
	progress = cliui.NewProgress(&redirected, cliui.ModeHuman, cliui.Capabilities{Interactive: false}, false)
	_ = progress.Start("Applying update")
	_ = progress.Stop(false, "Update failed")
	if redirected.Len() != 0 {
		t.Fatalf("redirected progress was not silent: %q", redirected.String())
	}
}
