//go:build windows

package interactiveserve

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// processAlive probes for pid's existence without terminating it, mirroring
// takeover.go's unix processAlive (Signal(0)). GetExitCodeProcess against
// STILL_ACTIVE is the standard Windows equivalent -- opening the handle
// alone doesn't tell you whether the process has already exited, since a
// pid can be reused after the process table entry is reclaimed, but a
// short-lived OpenProcess+GetExitCodeProcess pair right before use is the
// same best-effort liveness check every caller of this package already
// accepts on unix.
func processAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}
	return exitCode == 259 // STILL_ACTIVE
}

// maxAncestorWalkDepth bounds currentProcessIsDescendantOf's walk up the
// parent chain -- generous for any real process tree, but finite so a
// pathological or misread process chain can never spin forever. Same
// value and same reasoning as takeover.go's unix constant of the same
// name (kept separate per file/build-tag rather than shared, matching how
// idleTimeout-style constants in this package are already duplicated
// where platforms need independently tunable values -- these two happen
// to agree, not because they must).
const maxAncestorWalkDepth = 64

// currentProcessIsDescendantOf reports whether the calling process is a
// descendant of pid, walking the parent chain via a CreateToolhelp32Snapshot
// process-tree snapshot -- the Windows equivalent of takeover.go's unix
// `ps -o ppid=` walk. determined is false when the walk couldn't be
// completed (snapshot unavailable, target pid not found in it); Takeover
// treats "couldn't tell" as "proceed as before," matching the unix
// implementation's own stated policy: this check is a safety net against a
// real, observed failure mode, not a security gate that has to fail closed.
func currentProcessIsDescendantOf(pid int) (descendant, determined bool) {
	parents, ok := processParentMap()
	if !ok {
		return false, false
	}
	return currentProcessIsDescendantOfWithParents(pid, parents)
}

func currentProcessIsDescendantOfWithParents(pid int, parents map[uint32]uint32) (descendant, determined bool) {
	// Toolhelp records numeric parent PIDs, not process identities. If an
	// ancestor exited and Windows reused its PID for a newer process, a
	// snapshot can make that newer process look like our ancestor. A real
	// ancestor must have started before this process, so rule out a younger
	// target before trusting the PID chain.
	if younger, known := targetStartedAfterCaller(pid); known && younger {
		return false, true
	}
	return descendantInParentMap(windows.GetCurrentProcessId(), uint32(pid), parents)
}

func descendantInParentMap(current, target uint32, parents map[uint32]uint32) (descendant, determined bool) {
	for depth := 0; depth < maxAncestorWalkDepth; depth++ {
		parent, found := parents[current]
		if !found {
			return false, true
		}
		if parent == target {
			return true, true
		}
		if parent == 0 || parent == current {
			return false, true
		}
		current = parent
	}
	return false, true
}

func targetStartedAfterCaller(pid int) (younger, known bool) {
	target, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false, false
	}
	defer windows.CloseHandle(target)
	var targetCreated, currentCreated windows.Filetime
	var exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(target, &targetCreated, &exit, &kernel, &user); err != nil {
		return false, false
	}
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &currentCreated, &exit, &kernel, &user); err != nil {
		return false, false
	}
	return targetCreated.Nanoseconds() > currentCreated.Nanoseconds(), true
}

// processParentMap snapshots every running process's pid->parent-pid
// relationship in one pass, matching how a single `ps -ef` invocation
// captures the whole tree at once on unix rather than issuing one query
// per hop.
func processParentMap() (map[uint32]uint32, bool) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, false
	}
	defer windows.CloseHandle(snapshot)

	parents := make(map[uint32]uint32)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, false
	}
	for {
		parents[entry.ProcessID] = entry.ParentProcessID
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return parents, true
}
