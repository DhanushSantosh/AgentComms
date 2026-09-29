//go:build !windows && !js

package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/store"
)

// TestDoctorFixDoesNotClaimNothingToRepairWhenARepairFailed is the
// regression test for a report that contradicted itself. `doctor --fix`
// printed "Repaired: nothing to repair" directly above "Could not
// repair: project lifecycle is locked by another process", so the line
// a reader takes at face value said there had been nothing wrong. It
// was reported as a silent no-op precisely because the first line reads
// as one.
//
// The lock is what a real run contends with: a daemon reconciling the
// same project right after its recorded build ID changed. Held here for
// the whole call with flock, exactly as another process would.
func TestDoctorFixDoesNotClaimNothingToRepairWhenARepairFailed(t *testing.T) {
	project := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	cleanupProjectDaemon(t, project)
	var stdout, stderr bytes.Buffer
	run := func(args ...string) error {
		stdout.Reset()
		stderr.Reset()
		return Run(append(args, "--project", project, "--json"), &stdout, &stderr)
	}
	if err := run("init", "--non-interactive", "--owner", "owner"); err != nil {
		t.Fatal(err)
	}
	// A finding the lifecycle reconciliation is the repair for, so the
	// blocked lock is what stands between --fix and success.
	if err := os.Remove(filepath.Join(project, store.Runtime, "AGENT_INSTRUCTIONS.md")); err != nil {
		t.Fatal(err)
	}

	lockPath := filepath.Join(project, store.Runtime, "upgrade.lock")
	held, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	// flock is per open file description, not per process, so a second
	// descriptor on the same file contends with this one even though the
	// CLI runs in this same test binary.
	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("could not take the upgrade lock: %v", err)
	}
	defer func() {
		_ = syscall.Flock(int(held.Fd()), syscall.LOCK_UN)
		_ = held.Close()
	}()

	// --timeout 1s so the lock wait is short; the point is the report it
	// produces once the wait fails, not how long it waits.
	if err := run("doctor", "--fix", "--timeout", "1s"); err != nil {
		t.Fatalf("doctor --fix should still report, not error out: %v (%s)", err, stderr.String())
	}
	var envelope struct {
		Result struct {
			Fixed     []string `json:"fixed"`
			FixErrors []string `json:"fix_errors"`
		} `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("parse doctor output: %v\n%s", err, stdout.String())
	}
	// The daemon repair is a separate remedy and legitimately succeeds
	// here; what must not be claimed is the reconciliation the lock
	// actually blocked.
	for _, claim := range envelope.Result.Fixed {
		if strings.Contains(claim, "reconciled the project lifecycle") {
			t.Errorf("the lock blocked the reconciliation, so it must not be reported as done: %q", claim)
		}
	}
	if len(envelope.Result.FixErrors) == 0 {
		t.Fatalf("a blocked repair must be reported as one:\n%s", stdout.String())
	}
	joined := strings.Join(envelope.Result.FixErrors, "; ")
	if !strings.Contains(joined, "locked by another process") {
		t.Errorf("the reason should name the lock, got %q", joined)
	}
	if !strings.Contains(joined, "re-run") {
		t.Errorf("the reader's next move is to wait and re-run; the message should say so, got %q", joined)
	}

	// The finding the lock blocked is still there, and still advertised
	// as repairable -- the reader is told to try again, not that their
	// project needs manual surgery.
	if !strings.Contains(stdout.String(), "AGENT_INSTRUCTIONS_MISSING") {
		t.Errorf("the blocked finding should still be reported:\n%s", stdout.String())
	}
}
