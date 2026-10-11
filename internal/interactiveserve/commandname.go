package interactiveserve

import (
	"runtime"
	"strings"
)

// CommandName returns the name a wrapped command is known by -- "claude",
// "codex", "opencode" -- from however it was spelled on the command line.
//
// Comparing filepath.Base(command[0]) against "claude" silently failed on
// Windows, where the same CLI is routinely written (or resolved) as
// claude.exe, and npm-installed CLIs are opencode.cmd / codex.cmd: resume
// pinning, session discovery and --claude-allow-agent-comms all treated a
// perfectly ordinary Windows invocation as an unknown command.
func CommandName(path string) string {
	return commandNameFor(path, runtime.GOOS)
}

func commandNameFor(path, goos string) string {
	separators := "/"
	if goos == "windows" {
		separators = `/\`
	}
	base := path
	if i := strings.LastIndexAny(base, separators); i >= 0 {
		base = base[i+1:]
	}
	if goos != "windows" {
		return base
	}
	lower := strings.ToLower(base)
	for _, ext := range []string{".exe", ".cmd", ".bat", ".com", ".ps1"} {
		if strings.HasSuffix(lower, ext) {
			return lower[:len(lower)-len(ext)]
		}
	}
	return lower
}
