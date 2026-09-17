package claudetail

import (
	"strings"
	"testing"
)

func TestFormatRendersAssistantTextBlock(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"GUAVA CONFIRMED"}]}}`)
	rendered, ok := Format(line)
	if !ok {
		t.Fatal("expected ok=true for an assistant text block")
	}
	if !strings.Contains(rendered, "ASSISTANT") || !strings.Contains(rendered, "GUAVA CONFIRMED") {
		t.Fatalf("unexpected rendering: %q", rendered)
	}
}

func TestFormatRendersToolUseBlock(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}]}}`)
	rendered, ok := Format(line)
	if !ok {
		t.Fatal("expected ok=true for a tool_use block")
	}
	if !strings.Contains(rendered, "[tool call: Bash]") {
		t.Fatalf("unexpected rendering: %q", rendered)
	}
}

func TestFormatRendersPlainStringUserContent(t *testing.T) {
	line := []byte(`{"type":"user","message":{"role":"user","content":"AXIOM, this is FIXER. Live demo: reply with exactly GUAVA CONFIRMED."}}`)
	rendered, ok := Format(line)
	if !ok {
		t.Fatal("expected ok=true for plain string user content")
	}
	if !strings.Contains(rendered, "USER") || !strings.Contains(rendered, "GUAVA CONFIRMED") {
		t.Fatalf("unexpected rendering: %q", rendered)
	}
}

func TestFormatSkipsBookkeepingAndAttachmentLines(t *testing.T) {
	for _, line := range [][]byte{
		[]byte(`{"type":"bridge-session"}`),
		[]byte(`{"type":"last-prompt"}`),
		[]byte(`{"type":"attachment"}`),
		[]byte(`not json at all`),
		[]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"step-start"}]}}`),
	} {
		if _, ok := Format(line); ok {
			t.Fatalf("expected ok=false for %q", line)
		}
	}
}
