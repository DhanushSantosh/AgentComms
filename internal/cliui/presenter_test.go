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

func TestPresenterRenderResultProducesDeterministicReadableTree(t *testing.T) {
	value := map[string]any{
		"task_id": "task-7\x1b]8;;https://evil.invalid\a",
		"status":  "CLAIMED",
		"lease": map[string]any{
			"owner": "builder",
			"until": "2026-08-26T10:30:00Z",
		},
		"resources": []string{"src/api", "docs"},
	}
	var output bytes.Buffer
	presenter := cliui.Presenter{Out: &output, Mode: cliui.ModePlain}
	if err := presenter.RenderResult("task.claim", value, nil); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"Task claim", "Status", "CLAIMED", "Task id", "task-7", "Lease", "Owner", "builder", "Resources (2)", "src/api"} {
		if !strings.Contains(got, want) {
			t.Fatalf("readable result is missing %q:\n%s", want, got)
		}
	}
	if strings.ContainsAny(got, "{}") || strings.Contains(got, "\x1b") || strings.Contains(got, "https://evil.invalid") {
		t.Fatalf("readable result leaked JSON or terminal controls:\n%q", got)
	}

	output.Reset()
	if err := presenter.RenderResult("invocation.request", map[string]any{"id": "inv-1"}, map[string]any{"status": "DELIVERED"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "Delivery") || !strings.Contains(got, "DELIVERED") {
		t.Fatalf("delivery result was not given a readable section:\n%s", got)
	}
}

func TestPresenterRenderTableAlignsDisplayCellsAndSanitizesValues(t *testing.T) {
	var output bytes.Buffer
	presenter := cliui.Presenter{Out: &output, Mode: cliui.ModePlain}
	err := presenter.RenderTable(cliui.Table{
		Headers: []string{"Agent", "Status"},
		Rows: [][]string{
			{"界", "ACTIVE"},
			{"builder\x1b[31m", "WAITING\nfor approval"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Agent    Status\n界       ACTIVE\nbuilder  WAITINGfor approval\n"
	if output.String() != want {
		t.Fatalf("table output mismatch:\n got %q\nwant %q", output.String(), want)
	}

	output.Reset()
	if err := presenter.RenderTable(cliui.Table{Headers: []string{"Agent"}}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "(no rows)\n" {
		t.Fatalf("empty table output mismatch: %q", output.String())
	}
}
