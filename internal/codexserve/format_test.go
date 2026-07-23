package codexserve

import (
	"strings"
	"testing"
)

func TestFormatRendersCompletedAssistantMessage(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"agentMessage","phase":"final_answer","text":"GUAVA CONFIRMED"}}}`)
	rendered, ok := Format(line)
	if !ok {
		t.Fatal("expected ok=true for a completed final_answer agentMessage")
	}
	if !strings.Contains(rendered, "ASSISTANT") || !strings.Contains(rendered, "GUAVA CONFIRMED") {
		t.Fatalf("unexpected rendering: %q", rendered)
	}
}

func TestFormatSkipsNonFinalAgentMessage(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"agentMessage","phase":"thinking","text":"..."}}}`)
	if _, ok := Format(line); ok {
		t.Fatal("expected ok=false for a non-final_answer agentMessage")
	}
}

func TestFormatRendersCompletedUserMessage(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"userMessage","content":[{"type":"text","text":"hello there"}]}}}`)
	rendered, ok := Format(line)
	if !ok {
		t.Fatal("expected ok=true for a completed userMessage")
	}
	if !strings.Contains(rendered, "USER") || !strings.Contains(rendered, "hello there") {
		t.Fatalf("unexpected rendering: %q", rendered)
	}
}

func TestFormatSkipsUnrelatedNotificationsAndResponses(t *testing.T) {
	for _, line := range [][]byte{
		[]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`),
		[]byte(`{"jsonrpc":"2.0","method":"item/started","params":{"item":{"type":"agentMessage"}}}`),
		[]byte(`{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"delta":"P"}}`),
		[]byte(`not json at all`),
	} {
		if _, ok := Format(line); ok {
			t.Fatalf("expected ok=false for %q", line)
		}
	}
}
