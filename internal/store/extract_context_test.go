package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestLegacyExtractionIsNonDurableAndUnverified(t *testing.T) {
	root := initProject(t)
	legacy := []byte("[agents]\nmentioned-agent: prose only\n[decisions]\nUse a stable wire format\n[contracts]\nReview before release\n")
	if e := os.WriteFile(filepath.Join(root, ".agents"), legacy, 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	s := Open(root)
	s.SetCredentialStore(identity.NewMemoryStore())
	if _, e := s.PrepareLegacyAdoption("owner"); e != nil {
		t.Fatal(e)
	}
	before, _ := s.Events()
	out, e := s.ExtractLegacyContext()
	if e != nil {
		t.Fatal(e)
	}
	after, _ := s.Events()
	if out.Durable || len(before) != len(after) {
		t.Fatalf("extraction changed durable history: before=%d after=%d", len(before), len(after))
	}
	if len(out.Candidates) != 2 || len(out.Agents) != 1 {
		t.Fatalf("unexpected candidates: %#v", out)
	}
	for _, candidate := range out.Candidates {
		if candidate.CurrentTruth || candidate.Confidence != "UNVERIFIED" {
			t.Fatalf("legacy candidate was promoted to truth: %#v", candidate)
		}
	}
}

func TestAutoSyncPullFailurePreventsEvent(t *testing.T) {
	root := initProject(t)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	s := Open(root)
	s.SetCredentialStore(identity.NewMemoryStore())
	if e := s.Init("owner"); e != nil {
		t.Fatal(e)
	}
	cfg, _ := s.Config()
	cfg.AutoSync = true
	if e := writeJSON(filepath.Join(root, Runtime, "config.json"), cfg, 0644); e != nil {
		t.Fatal(e)
	}
	if e := s.SetupRemote(filepath.Join(root, "missing-remote.git")); e != nil {
		t.Fatal(e)
	}
	before, _ := s.Events()
	if _, e := s.Append("owner", "decision.create", "must-not-commit", model.DecisionPayload{Title: "No", Statement: "Remote is unavailable"}); e == nil {
		t.Fatal("auto-sync failure was ignored")
	}
	after, _ := s.Events()
	if len(after) != len(before) {
		t.Fatal("event committed after failed auto-sync pull")
	}
}
