package service

import (
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestProjectSettingsRequireElevatedActorAndControlLease(t *testing.T) {
	instance := setup(t)
	activate(t, instance, "builder", model.PrincipalAgent)

	payload := model.ProjectSettingsUpdated{
		DefaultLease: "90m", StaleGrace: "30m", ActiveRetention: "720h",
		SummaryLimit: 2048, ArtifactLimitBytes: 8 * 1024 * 1024, RequireReview: true,
	}
	if _, err := instance.Execute("builder", "project.settings.update", "project", payload); err == nil {
		t.Fatal("agent changed governed project policy")
	}
	if _, err := instance.Execute("owner", "project.settings.update", "project", payload); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Execute("owner", "task.create", "task-settings", model.TaskCreated{
		Title: "Verify settings", Repository: "repo", Branch: "main", Resources: []string{"src"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Execute("builder", "task.claim", "task-settings", model.TaskClaimed{}); err != nil {
		t.Fatal(err)
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	remaining := time.Until(state.Tasks["task-settings"].LeaseUntil)
	if remaining < 89*time.Minute || remaining > 91*time.Minute {
		t.Fatalf("lease duration=%s, want 90m", remaining)
	}
}

func TestProjectSettingsRejectUnsafeBounds(t *testing.T) {
	instance := setup(t)
	_, err := instance.Execute("owner", "project.settings.update", "project", model.ProjectSettingsUpdated{
		DefaultLease: "1m", StaleGrace: "1h", ActiveRetention: "168h",
		SummaryLimit: 1200, ArtifactLimitBytes: 5 * 1024 * 1024,
	})
	if err == nil {
		t.Fatal("unsafe lease was accepted")
	}
}
