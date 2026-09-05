package cliui_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
)

// TestRenderTableDropsLastColumnFirstWithNoExplicitPriorities documents
// RenderTable's existing default (Priorities nil -> priority == column
// index -> last column removed first under width pressure) -- the exact
// behavior UX-04 found wrong specifically for message inbox's SUBJECT
// column, fixed there by supplying explicit Priorities rather than
// changing this default every other caller still relies on.
func TestRenderTableDropsLastColumnFirstWithNoExplicitPriorities(t *testing.T) {
	var out bytes.Buffer
	presenter := cliui.Presenter{Out: &out, Mode: cliui.ModeHuman, Capabilities: cliui.Capabilities{Width: 20}}
	err := presenter.RenderTable(cliui.Table{
		Headers: []string{"ID", "LAST"},
		Rows:    [][]string{{"row-1-long-id-value", "kept"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "LAST") {
		t.Fatalf("expected the last column to be dropped with no explicit priorities, got:\n%s", out.String())
	}
}

// TestRenderTableRespectsExplicitPriorities is the regression test for
// UX-04's actual fix: a caller can protect a column regardless of its
// position by supplying Priorities, so a narrow terminal drops the
// column marked most disposable (highest priority number) instead of
// whichever one happens to be listed last.
func TestRenderTableRespectsExplicitPriorities(t *testing.T) {
	var out bytes.Buffer
	presenter := cliui.Presenter{Out: &out, Mode: cliui.ModeHuman, Capabilities: cliui.Capabilities{Width: 20}}
	err := presenter.RenderTable(cliui.Table{
		Headers:    []string{"ID", "SUBJECT"},
		Priorities: []int{1, 0}, // ID (index 0) is the most disposable here.
		Rows:       [][]string{{"row-1-long-id-value", "kept"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "row-1-long-id-value") {
		t.Fatalf("expected ID to be dropped per explicit priorities, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "SUBJECT") {
		t.Fatalf("expected SUBJECT to survive per explicit priorities, got:\n%s", out.String())
	}
}

// TestRenderTableNeverTruncatesAtZeroWidth confirms the non-interactive
// path CapabilitiesFor now produces (Width: 0) actually disables
// truncation in RenderTable, not just in theory.
func TestRenderTableNeverTruncatesAtZeroWidth(t *testing.T) {
	var out bytes.Buffer
	presenter := cliui.Presenter{Out: &out, Mode: cliui.ModePlain, Capabilities: cliui.Capabilities{Width: 0}}
	err := presenter.RenderTable(cliui.Table{
		Headers: []string{"ID", "KIND", "FROM", "STATUS", "SUBJECT"},
		Rows:    [][]string{{"a-very-long-machine-generated-message-id-value", "ACTION", "someone", "OPEN", "A subject worth reading in full"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"a-very-long-machine-generated-message-id-value", "A subject worth reading in full"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q to survive at Width=0, got:\n%s", want, out.String())
		}
	}
}
