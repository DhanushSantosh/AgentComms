package service_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/DhanushSantosh/AgentComms/internal/testsupport"
)

// TestDeleteProjectRequiresOwnerRole confirms the OWNER-only gate holds
// even for an actor who has their own registered elevated key -- role is
// checked first and independently, not inferred from "has an elevated
// key." See RFC 0020.
func TestDeleteProjectRequiresOwnerRole(t *testing.T) {
	s, root := testsupport.StartPersonalProject(t)
	activate(t, s, "member", model.PrincipalHuman)
	if _, err := s.ElevateKey("member", "a strong passphrase"); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Store.Config()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteProject("member", "a strong passphrase", cfg.ProjectID); err == nil {
		t.Fatal("expected a non-owner actor to be refused")
	}
	requireRuntimeSurvives(t, root)
}

// TestDeleteProjectRequiresConfirmProjectID is the regression test for the
// typed-confirmation step: a mismatched value must refuse before anything
// is touched, catching a fat-fingered confirmation before it ever reaches
// the passphrase prompt.
func TestDeleteProjectRequiresConfirmProjectID(t *testing.T) {
	s, root := testsupport.StartPersonalProject(t)
	if _, err := s.ElevateKey("owner", "a strong passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteProject("owner", "a strong passphrase", "not-the-real-project-id"); err == nil {
		t.Fatal("expected a mismatched confirmation to be refused")
	}
	requireRuntimeSurvives(t, root)
}

func TestDeleteProjectRequiresElevatedKey(t *testing.T) {
	s, root := testsupport.StartPersonalProject(t)
	cfg, err := s.Store.Config()
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DeleteProject("owner", "whatever", cfg.ProjectID)
	if err == nil {
		t.Fatal("expected deletion to be refused with no elevated key registered")
	}
	if !strings.Contains(err.Error(), "elevated key") {
		t.Fatalf("expected an elevated-key-shaped error, got: %v", err)
	}
	requireRuntimeSurvives(t, root)
}

func TestDeleteProjectWrongPassphraseFails(t *testing.T) {
	s, root := testsupport.StartPersonalProject(t)
	if _, err := s.ElevateKey("owner", "correct passphrase"); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Store.Config()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteProject("owner", "wrong passphrase", cfg.ProjectID); err == nil {
		t.Fatal("expected a wrong passphrase to be refused")
	}
	requireRuntimeSurvives(t, root)
}

// TestDeleteProjectPersonalModeSuccess is the full happy-path end to end:
// owner, correct confirmation, correct passphrase -- the runtime directory
// and .agents bootstrap file are actually gone from disk afterward, and
// the result correctly reports no remote deletion happened (personal
// mode has no authority to delete from).
func TestDeleteProjectPersonalModeSuccess(t *testing.T) {
	s, root := testsupport.StartPersonalProject(t)
	if _, err := s.ElevateKey("owner", "correct passphrase"); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Store.Config()
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.DeleteProject("owner", "correct passphrase", cfg.ProjectID)
	if err != nil {
		t.Fatalf("expected deletion to succeed, got %v", err)
	}
	if !result.RuntimeRemoved || !result.BootstrapRemoved {
		t.Fatalf("expected both runtime and bootstrap removal to succeed, got %+v", result)
	}
	if result.RemoteDeleted {
		t.Fatal("personal mode must never report a remote deletion")
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected a clean success with no warnings, got %v", result.Warnings)
	}
	if _, statErr := os.Stat(filepath.Join(root, store.Runtime)); !os.IsNotExist(statErr) {
		t.Fatalf("expected the runtime directory to be gone, stat returned: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".agents")); !os.IsNotExist(statErr) {
		t.Fatalf("expected .agents to be gone, stat returned: %v", statErr)
	}
}

func requireRuntimeSurvives(t *testing.T, root string) {
	t.Helper()
	if _, statErr := os.Stat(filepath.Join(root, store.Runtime)); statErr != nil {
		t.Fatalf("expected the runtime directory to survive a refused delete: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".agents")); statErr != nil {
		t.Fatalf("expected .agents to survive a refused delete: %v", statErr)
	}
}
