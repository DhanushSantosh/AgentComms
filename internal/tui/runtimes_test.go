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

	// agy support was removed 2026-08-08; a stale local binding from before
	// that (or any other adapter this view has no dedicated label for) must
	// still display honestly via the raw adapter string, not error out or
	// disappear.
	if err := sessionbind.Save(source.root, "hulk-runtime-1", "5885f88c-6cdf-4343-ad9d-693e66d41852", "agy"); err != nil {
		t.Fatal(err)
	}
	provider, session = source.sessionBinding("hulk-runtime-1")
	if provider != "agy" || session != "5885f88c-6cdf-4343-ad9d-693e66d41852" {
		t.Fatalf("expected the raw adapter string for an unmapped/removed adapter, got provider=%q session=%q", provider, session)
	}

	if err := sessionbind.Save(source.root, "peter-runtime-1", "ses_032d59696ffepgBGk73AiMF00F", "opencode"); err != nil {
		t.Fatal(err)
	}
	provider, session = source.sessionBinding("peter-runtime-1")
	if provider != "OpenCode" || session != "ses_032d59696ffepgBGk73AiMF00F" {
		t.Fatalf("expected the full bound opencode provider and session ID, got provider=%q session=%q", provider, session)
	}

	provider, session = source.sessionBinding("unregistered-runtime")
	if provider != "—" || session != "unbound" {
		t.Fatalf("expected unbound for a different runtime, got provider=%q session=%q", provider, session)
	}
}

// TestRuntimesViewRendersDetailPaneForSelectedRow confirms the full panel
// (compact table + detail pane) renders together and shows the selected
// row's full picture -- the actual UX outcome of the master-detail
// redesign, not just the underlying data functions in isolation.
func TestRuntimesViewRendersDetailPaneForSelectedRow(t *testing.T) {
	instance := newTestService(t)
	registerAgent(t, instance, "AXIOM", model.Role("MEMBER"), "src")
	if _, err := instance.Execute("AXIOM", "runtime.register", "axiom-runtime-1",
		model.RuntimeRegistered{AgentID: "AXIOM", Connector: "MANUAL", MaxConcurrent: 1}); err != nil {
		t.Fatal(err)
	}
	if err := sessionbind.Save(instance.Store.Root, "axiom-runtime-1", "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475", "claude"); err != nil {
		t.Fatal(err)
	}
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.openView("Runtimes")
	view.rowFocus = true
	view.runtimeList.Refresh(view.state, view.actor)
	rendered := view.View().Content
	for _, expected := range []string{"AXIOM", "RUNTIME DETAIL", "Claude", "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475"} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("runtimes view missing %q:\n%s", expected, rendered)
		}
	}
}

// TestRuntimeDetailIncludesProviderAndSession is the successor to what was
// TestRuntimeRowsIncludeProviderAndSessionColumns: provider/session binding
// moved out of the table (runtimeRowSource.Columns is now just the
// essentials -- see the doc comment on it) and into the per-row detail pane
// computed by detailFor, so this now asserts against detailFor's result
// instead of a table row.
func TestRuntimeDetailIncludesProviderAndSession(t *testing.T) {
	instance := newTestService(t)
	registerAgent(t, instance, "AXIOM", model.Role("MEMBER"), "src")
	if _, err := instance.Execute("AXIOM", "runtime.register", "axiom-runtime-1",
		model.RuntimeRegistered{AgentID: "AXIOM", Connector: "MANUAL", MaxConcurrent: 1}); err != nil {
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
	detail, ok := source.detailFor("axiom-runtime-1", state)
	if !ok {
		t.Fatal("expected detailFor to find axiom-runtime-1")
	}
	if detail.provider != "Claude" || detail.session != "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475" {
		t.Fatalf("expected Claude provider and full session ID, got provider=%q session=%q", detail.provider, detail.session)
	}
}

// TestRuntimeColumnsAreCompact confirms the table itself stays to the
// essentials now that everything else lives in the detail pane -- a wide
// terminal used to be required just to read the table at all.
func TestRuntimeColumnsAreCompact(t *testing.T) {
	columns := runtimeRowSource{}.Columns(160)
	var titles []string
	for _, column := range columns {
		titles = append(titles, column.Title)
	}
	got := strings.Join(titles, "|")
	want := "STATUS|HEALTH|AGENT|KIND"
	if got != want {
		t.Fatalf("columns = %q, want %q", got, want)
	}
}
