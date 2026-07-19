package tui

import (
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestSettingsWorkspaceIsResponsiveAndActionable(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	for index, name := range views {
		if name == "Project settings" {
			view.view, view.cursor = index, index
		}
	}
	for _, width := range []int{140, 92, 62} {
		view.width, view.height = width, 38
		rendered := view.View().Content
		for _, expected := range []string{"Project policy", "SIGNED GOVERNANCE", "Default task lease"} {
			if !strings.Contains(rendered, expected) {
				t.Errorf("width %d missing %q", width, expected)
			}
		}
	}
	view.width = 140
	if rendered := view.View().Content; !strings.Contains(rendered, "hidden by default") {
		t.Fatal("wide settings workspace exposed no hidden-storage guidance")
	}
}

func TestSettingsFormPublishesSignedPolicy(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	next, _ := view.openProjectSettingsForm()
	view = next.(Model)
	values := []string{"2h", "20m", "336h", "2400", "7", "true"}
	for index, value := range values {
		view.inputs[index].SetValue(value)
	}
	view.formFocus = len(view.inputs) - 1
	view = pressKey(t, view, keyEnter())
	if view.confirm == nil {
		t.Fatal("shared setting change did not require confirmation")
	}
	view = pressKey(t, view, keyEnter())
	if view.err != nil {
		t.Fatal(view.err)
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	want := model.ProjectSettings{
		DefaultLease: "2h", StaleGrace: "20m", ActiveRetention: "336h",
		SummaryLimit: 2400, ArtifactLimitBytes: 7 * 1024 * 1024, RequireReview: true,
	}
	got := state.ProjectSettings
	got.UpdatedAt = want.UpdatedAt
	got.UpdatedBy = want.UpdatedBy
	if got != want {
		t.Fatalf("settings=%+v, want %+v", got, want)
	}
}
