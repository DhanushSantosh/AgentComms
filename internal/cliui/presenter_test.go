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
