package claudetail

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type synchronizedBuffer struct {
	mu     sync.RWMutex
	buffer bytes.Buffer
}

func (b *synchronizedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *synchronizedBuffer) String() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.buffer.String()
}

func TestSessionPathMatchesRealClaudeCodeLayout(t *testing.T) {
	got, err := SessionPath("/home/dhanush/.claude", "/home/dhanush/Projects/DeskCrafter", "a09c5424-1723-4ee9-8fb2-8d2625393561")
	if err != nil {
		t.Fatal(err)
	}
	want := "/home/dhanush/.claude/projects/-home-dhanush-Projects-DeskCrafter/a09c5424-1723-4ee9-8fb2-8d2625393561.jsonl"
	if got != want {
		t.Fatalf("SessionPath() = %q, want %q", got, want)
	}
}

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

func TestTailReplaysExistingHistoryThenFollowsAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	initial := `{"type":"user","message":{"role":"user","content":"first turn"}}` + "\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var out synchronizedBuffer
	done := make(chan error, 1)
	go func() { done <- Tail(ctx, path, &out, true) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(out.String(), "first turn") {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "first turn") {
		t.Fatalf("expected replayed history to contain %q, got %q", "first turn", out.String())
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"second turn"}]}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(out.String(), "second turn") {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "second turn") {
		t.Fatalf("expected a live-appended turn to contain %q, got %q", "second turn", out.String())
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTailWithoutReplaySkipsExistingHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"should not appear"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- Tail(ctx, path, &out, false) }()

	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	if strings.Contains(out.String(), "should not appear") {
		t.Fatalf("expected no-replay tail to skip existing history, got %q", out.String())
	}
}
