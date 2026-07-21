package tui

import (
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/sessionbind"
)

func TestRuntimeSessionBindingReflectsCapturedProviderAndID(t *testing.T) {
	source := runtimeRowSource{root: t.TempDir()}
	provider, session := source.sessionBinding("axiom-runtime-1")
	if provider != "—" || session != "unbound" {
		t.Fatalf("expected unbound before any capture, got provider=%q session=%q", provider, session)
	}
	if err := sessionbind.Save(source.root, "axiom-runtime-1", "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475", "claude"); err != nil {
		t.Fatal(err)
	}
	provider, session = source.sessionBinding("axiom-runtime-1")
	if provider != "Claude" || session != "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475" {
		t.Fatalf("expected the full bound provider and session ID, got provider=%q session=%q", provider, session)
	}

	if err := sessionbind.Save(source.root, "damon-runtime-1", "019e5408-3ef4-7db3-b584-03ad8f399199", "codex"); err != nil {
		t.Fatal(err)
	}
	provider, session = source.sessionBinding("damon-runtime-1")
	if provider != "Codex" || session != "019e5408-3ef4-7db3-b584-03ad8f399199" {
		t.Fatalf("expected the full bound codex provider and thread ID, got provider=%q session=%q", provider, session)
	}

	provider, session = source.sessionBinding("unregistered-runtime")
	if provider != "—" || session != "unbound" {
		t.Fatalf("expected unbound for a different runtime, got provider=%q session=%q", provider, session)
	}
}

func TestRuntimeRowsIncludeProviderAndSessionColumns(t *testing.T) {
	instance := newTestService(t)
	registerAgent(t, instance, "axiom", model.RoleAgent, "src")
	if _, err := instance.Execute("axiom", "runtime.register", "axiom-runtime-1",
		model.RuntimeRegistered{AgentID: "axiom", Connector: "MANUAL", MaxConcurrent: 1}); err != nil {
		t.Fatal(err)
	}
	if err := sessionbind.Save(instance.Store.Root, "axiom-runtime-1", "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475", "claude"); err != nil {
		t.Fatal(err)
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	source := runtimeRowSource{root: instance.Store.Root}
	rows := source.Rows(state, "owner", false)
	if len(rows) != 1 {
		t.Fatalf("expected one runtime row, got %d", len(rows))
	}
	row := strings.Join(rows[0], "|")
	if !strings.Contains(row, "Claude") || !strings.Contains(row, "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475") {
		t.Fatalf("expected the provider and full session ID in the row, got: %q", row)
	}
}

func TestRuntimeColumnsIncludeProviderAndSessionHeaders(t *testing.T) {
	columns := runtimeRowSource{}.Columns(160)
	var titles []string
	for _, column := range columns {
		titles = append(titles, column.Title)
	}
	joined := strings.Join(titles, "|")
	if !strings.Contains(joined, "PROVIDER") || !strings.Contains(joined, "SESSION") {
		t.Fatalf("expected PROVIDER and SESSION columns, got: %q", joined)
	}
}
