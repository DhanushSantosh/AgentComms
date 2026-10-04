package codexserve

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/brokeridentity"
)

func TestBrokerProjectRuntimeNamesDoNotCollide(t *testing.T) {
	t.Setenv("AGENTCOMMS_FAKE_CODEX_PROCESS", "1")
	broker := NewBroker()
	defer broker.Close()
	server := httptest.NewServer(broker.Handler())
	defer server.Close()
	client, err := NewForProject(server.URL, "project-a")
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewForProject(server.URL, "project-b")
	if err != nil {
		t.Fatal(err)
	}
	first := fakeProcessConfig(t)
	second := first
	second.WorkDir = t.TempDir()
	if _, err := client.Register(context.Background(), "reviewer-runtime", first); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Register(context.Background(), "reviewer-runtime", second); err != nil {
		t.Fatalf("independent projects collide at shared broker: %v", err)
	}
	if _, err := client.Register(context.Background(), "reviewer-runtime", second); err == nil {
		t.Fatal("same-project conflict was accepted")
	}
	keyA, _ := brokeridentity.RuntimeKey("project-a", "reviewer-runtime")
	keyB, _ := brokeridentity.RuntimeKey("project-b", "reviewer-runtime")
	if broker.process(keyA) == nil || broker.process(keyB) == nil || broker.process(keyA) == broker.process(keyB) {
		t.Fatal("projects did not receive distinct child processes")
	}
	if output, err := client.Prompt(context.Background(), "reviewer-runtime", "warmup"); err != nil || output != "turn 1" {
		t.Fatalf("warmup: %q %v", output, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	eventsA, err := client.Subscribe(ctx, "reviewer-runtime")
	if err != nil {
		t.Fatal(err)
	}
	eventsB, err := other.Subscribe(ctx, "reviewer-runtime")
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		output   string
		err      error
		expected string
	}
	results := make(chan result, 2)
	go func() {
		out, err := client.Prompt(ctx, "reviewer-runtime", "project A")
		results <- result{out, err, "turn 2"}
	}()
	go func() {
		out, err := other.Prompt(ctx, "reviewer-runtime", "project B")
		results <- result{out, err, "turn 1"}
	}()
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil || r.output != r.expected {
			t.Fatalf("concurrent project turn: %+v", r)
		}
	}
	for _, stream := range []struct {
		events    <-chan []byte
		expected  string
		forbidden string
	}{
		{eventsA, "turn 2", ""}, {eventsB, "turn 1", "turn 2"},
	} {
		found := false
		for !found {
			select {
			case event, ok := <-stream.events:
				if !ok {
					t.Fatal("stream closed before its project event")
				}
				if stream.forbidden != "" && strings.Contains(string(event), stream.forbidden) {
					t.Fatal("cross-project event")
				}
				found = strings.Contains(string(event), stream.expected)
			case <-ctx.Done():
				t.Fatal("project subscriber missed its event")
			}
		}
	}
	if _, err := New(server.URL).Prompt(context.Background(), "reviewer-runtime", "hello"); err == nil {
		t.Fatal("scoped runtime leaked into legacy namespace")
	}
	legacy := New(server.URL)
	if _, err := legacy.Register(context.Background(), "legacy-runtime", first); err != nil {
		t.Fatal(err)
	}
	if out, err := legacy.Prompt(ctx, "legacy-runtime", "legacy"); err != nil || out != "turn 1" {
		t.Fatalf("legacy routing: %q %v", out, err)
	}
	if _, err := client.Prompt(ctx, "legacy-runtime", "scoped"); err == nil {
		t.Fatal("scoped prompt fell back to legacy process")
	}
	if _, err := client.Subscribe(ctx, "legacy-runtime"); err == nil {
		t.Fatal("scoped subscriber fell back to legacy process")
	}
	if _, err := NewForProject(server.URL, " "); err == nil {
		t.Fatal("blank project accepted")
	}
	if _, err := client.Prompt(ctx, "../invalid", "invalid"); err == nil {
		t.Fatal("invalid logical runtime accepted")
	}
}
