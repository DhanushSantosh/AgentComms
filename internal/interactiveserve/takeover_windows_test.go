//go:build windows

package interactiveserve

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// spawnLongRunning starts a real, detached, long-running Windows process
// (ping against localhost, a fixed count comfortably longer than any test
// needs) with CREATE_NEW_PROCESS_GROUP -- matching how a real
// interactive-serve wrapper is spawned (see claudeserve/codexserve/
// opencodeclient's detach_windows.go) -- and returns its pid. Unlike unix's
// takeover_test.go, there's no zombie-reaping concern to design around
// here: Windows doesn't require a parent to Wait() on an unrelated
// background process the way unix does.
func spawnLongRunning(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("ping.exe", "-n", "60", "127.0.0.1")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if processAlive(cmd.Process.Pid) {
			_ = cmd.Process.Kill()
		}
	})
	return cmd.Process.Pid
}

func TestTakeoverTerminatesALiveProcess(t *testing.T) {
	pid := spawnLongRunning(t)
	if !processAlive(pid) {
		t.Fatal("expected the spawned process to be alive before Takeover")
	}
	if err := Takeover(pid, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if processAlive(pid) {
		t.Fatal("expected the process to be gone after Takeover")
	}
}

func TestTakeoverOnAnAlreadyGonePIDSucceeds(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := Takeover(cmd.Process.Pid, time.Second); err != nil {
		t.Fatalf("expected Takeover on an already-exited pid to succeed, got: %v", err)
	}
}

// TestTakeoverRefusesWhenCallerIsADescendantOfTarget mirrors
// takeover_test.go's identically named unix test: os.Getppid() is real,
// guaranteed ancestry without needing to fabricate a fake process tree, and
// since the ancestry check runs before Takeover ever calls TerminateProcess,
// this never actually touches the test's real parent process.
func TestTakeoverRefusesWhenCallerIsADescendantOfTarget(t *testing.T) {
	if _, determined := currentProcessIsDescendantOf(os.Getppid()); !determined {
		t.Skip("Toolhelp32Snapshot-based ancestry check unavailable in this environment")
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
		t.Skip("Toolhelp32Snapshot-based ancestry check unavailable in this environment")
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
	pid := spawnLongRunning(t)
	descendant, determined := currentProcessIsDescendantOf(pid)
	if !determined {
		t.Skip("Toolhelp32Snapshot-based ancestry check unavailable in this environment")
	}
	if descendant {
		t.Fatal("expected the test process not to be a descendant of its own child")
	}
}
