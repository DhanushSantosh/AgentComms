//go:build !windows

package interactiveserve

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTakeoverTerminatesALiveProcess(t *testing.T) {
	// Spawn sleep as an orphan (its direct parent, this sh, exits
	// immediately and is reaped normally, leaving sleep reparented to
	// init) rather than as this test's own direct child. Takeover's real
	// target is always someone else's process -- a shell, a terminal
	// emulator -- never a child of the agent-comms process calling it; a
	// direct child would become a zombie only this test could reap after
	// SIGKILL, which the real use case never has to contend with.
	// The background job's stdio is redirected away from this command's own
	// pipe -- otherwise Output() blocks until sleep itself exits, since it
	// would inherit and hold open the same pipe this sh process's stdout
	// writes "echo $!" through.
	out, err := exec.Command("sh", "-c", "sleep 100 >/dev/null 2>&1 & echo $!").Output()
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	if !processAlive(pid) {
		t.Fatal("expected the spawned process to be alive before Takeover")
	}
	if err := Takeover(pid, time.Second); err != nil {
		t.Fatal(err)
	}
	if processAlive(pid) {
		t.Fatal("expected the process to be gone after Takeover")
	}
}

func TestTakeoverOnAnAlreadyGonePIDSucceeds(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := Takeover(cmd.Process.Pid, time.Second); err != nil {
		t.Fatalf("expected Takeover on an already-exited pid to succeed, got: %v", err)
	}
}
