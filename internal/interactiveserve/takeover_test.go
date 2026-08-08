//go:build !windows

package interactiveserve

import (
	"os"
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

// TestTakeoverRefusesWhenCallerIsADescendantOfTarget guards the actual fix:
// confirmed live 2026-08-07, an agent self-relaunching through its own Bash
// tool (a child of the session being taken over) took its own controlling
// terminal down with it. os.Getppid() is real, guaranteed ancestry (this
// test process genuinely is a child of it) without needing to fabricate a
// fake process tree -- and since the ancestry check runs before Takeover
// ever sends a real signal, this never actually touches the test's real
// parent process.
func TestTakeoverRefusesWhenCallerIsADescendantOfTarget(t *testing.T) {
	if _, determined := currentProcessIsDescendantOf(os.Getppid()); !determined {
		t.Skip("ps-based ancestry check unavailable in this environment")
	}
	err := Takeover(os.Getppid(), time.Second)
	if err == nil {
		t.Fatal("expected Takeover to refuse when the calling process is a descendant of the target")
	}
	if !strings.Contains(err.Error(), "--launch-terminal") {
		t.Fatalf("expected the refusal to mention --launch-terminal, got: %v", err)
	}
}

func TestCurrentProcessIsDescendantOfOwnParent(t *testing.T) {
	descendant, determined := currentProcessIsDescendantOf(os.Getppid())
	if !determined {
		t.Skip("ps-based ancestry check unavailable in this environment")
	}
	if !descendant {
		t.Fatal("expected the test process to be recognized as a descendant of its own parent")
	}
}

// TestCurrentProcessIsNotDescendantOfItsOwnChild uses a process this test
// itself just spawned as "obviously unrelated" ground truth -- the test
// process can never be a descendant of its own child, so this direction is
// unambiguous without depending on which real processes happen to be
// running on the machine.
func TestCurrentProcessIsNotDescendantOfItsOwnChild(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	descendant, determined := currentProcessIsDescendantOf(cmd.Process.Pid)
	if !determined {
		t.Skip("ps-based ancestry check unavailable in this environment")
	}
	if descendant {
		t.Fatal("expected the test process not to be a descendant of its own child")
	}
}
