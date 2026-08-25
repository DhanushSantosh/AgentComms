package cliui_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
)

func TestPresenterPlainOutputRemovesTerminalControlSequences(t *testing.T) {
	var out bytes.Buffer
	presenter := cliui.Presenter{Out: &out, Mode: cliui.ModePlain}
	err := presenter.Render(cliui.Document{
		Title: "\x1b[31mResult\x1b[0m",
		Fields: []cliui.Field{
			{Label: "Actor\nforged", Value: "\x1b]8;;https://example.invalid\aowner\x1b]8;;\a"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	if strings.ContainsAny(rendered, "\x1b\a\r") {
		t.Fatalf("plain output retained terminal control bytes: %q", rendered)
	}
	if strings.Contains(rendered, "Actor\nforged") {
		t.Fatalf("field label injected a new output line: %q", rendered)
	}
	if !strings.Contains(rendered, "Result") || !strings.Contains(rendered, "owner") {
		t.Fatalf("sanitization removed visible content: %q", rendered)
	}
}

func TestPresenterHumanOutputUsesSemanticStyleOnlyWithTerminalCapabilities(t *testing.T) {
	document := cliui.Document{
		Title:  "Artifact verified",
		Status: cliui.StatusSuccess,
		Fields: []cliui.Field{{Label: "SHA-256", Value: "abc123"}},
	}

	var rich bytes.Buffer
	err := (cliui.Presenter{
		Out:  &rich,
		Mode: cliui.ModeHuman,
		Capabilities: cliui.Capabilities{
			Interactive: true,
			Color:       true,
			Unicode:     true,
			Width:       100,
		},
	}).Render(document)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rich.String(), "\x1b[") {
		t.Fatalf("capable human terminal did not receive semantic color: %q", rich.String())
	}
	if !strings.Contains(rich.String(), "✓") {
		t.Fatalf("capable human terminal did not receive success glyph: %q", rich.String())
	}

	var redirected bytes.Buffer
	err = (cliui.Presenter{
		Out:          &redirected,
		Mode:         cliui.ModeHuman,
		Capabilities: cliui.Capabilities{Interactive: false, Color: true, Unicode: true, Width: 100},
	}).Render(document)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(redirected.String(), "\x1b[") || strings.Contains(redirected.String(), "✓") {
		t.Fatalf("redirected human output contained terminal decoration: %q", redirected.String())
	}
}

func TestCapabilitiesForHonorsTerminalFallbacksAndColorPolicy(t *testing.T) {
	capabilities := cliui.CapabilitiesFor(cliui.TerminalContext{
		Interactive: true,
		Width:       120,
		Term:        "xterm-256color",
		Locale:      "en_US.UTF-8",
	})
	if !capabilities.Interactive || !capabilities.Color || !capabilities.Unicode || capabilities.Width != 120 {
		t.Fatalf("unexpected capable terminal result: %#v", capabilities)
	}

	noColor := cliui.CapabilitiesFor(cliui.TerminalContext{
		Interactive: true,
		Width:       120,
		Term:        "xterm-256color",
		Locale:      "en_US.UTF-8",
		NoColor:     true,
	})
	if noColor.Color {
		t.Fatalf("NO_COLOR policy was ignored: %#v", noColor)
	}

	dumb := cliui.CapabilitiesFor(cliui.TerminalContext{
		Interactive: true,
		Term:        "dumb",
		Locale:      "C",
	})
	if dumb.Interactive || dumb.Color || dumb.Unicode || dumb.Width != 80 {
		t.Fatalf("dumb terminal did not fall back to deterministic plain capabilities: %#v", dumb)
	}
}
