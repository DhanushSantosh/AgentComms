package tui

import (
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/sessionbind"
)

func TestRuntimeSessionLabelReflectsCapturedBinding(t *testing.T) {
	source := runtimeRowSource{root: t.TempDir()}
	if got := source.sessionLabel("axiom-runtime-1"); got != "unbound" {
		t.Fatalf("expected unbound before any capture, got %q", got)
	}
	if err := sessionbind.Save(source.root, "axiom-runtime-1", "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475", "claude"); err != nil {
		t.Fatal(err)
	}
	if got := source.sessionLabel("axiom-runtime-1"); got != "claude:e22cbdad" {
		t.Fatalf("expected a truncated bound label, got %q", got)
	}
	if got := source.sessionLabel("damon-runtime-1"); got != "unbound" {
		t.Fatalf("expected unbound for a different runtime, got %q", got)
	}
}

func TestRuntimeRowsIncludeBoundSessionColumn(t *testing.T) {
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
	if !strings.Contains(row, "claude:e22cbdad") {
		t.Fatalf("expected the session column to show the bound session, got: %q", row)
	}
}
