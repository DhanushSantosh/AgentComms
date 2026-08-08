package interactiveserve

import "path/filepath"

// resumeSpec describes how one adapter's CLI accepts an explicit, pinned
// session/conversation ID to resume, as opposed to its own implicit "most
// recent conversation in this directory" behavior.
//
// The implicit mode is what interactive-serve's --takeover-pid respawn has
// relied on since it was built, and it is unsafe across a kill/respawn
// boundary: confirmed live 2026-08-07 by asking PETER (running opencode)
// directly, from its own CLI's vantage point, rather than guessing --
// opencode has no implicit resume at all, a bare relaunch always starts a
// brand new session, full stop, unless --session is passed. Claude Code's
// own --help draws the same line explicitly: -c/--continue is "the most
// recent conversation in the current directory" (recency-based), while
// -r/--resume takes an exact session ID. See docs/backlog.md's
// "Interactive-serve session pinning" entry for the fuller writeup. (An agy
// entry lived here too, briefly -- removed 2026-08-08 along with the rest
// of agy support, over an unresolved third-party ToS compliance question;
// same doc.)
type resumeSpec struct {
	// implicitFlags are argv tokens meaning "resume the most recent
	// session" -- stripped when an explicit sessionID is being pinned in
	// instead, so a caller's already-typed --continue doesn't sit
	// alongside (and potentially fight) the explicit flag this appends.
	implicitFlags []string
	// explicitFlag is the flag that takes a session/conversation ID as its
	// next argv value.
	explicitFlag string
}

// resumeSpecs covers every adapter this project currently wraps under
// interactive-serve. codex is deliberately omitted: it isn't run under
// interactive-serve by any live runtime today, and its resume mechanic
// (`codex resume <id>`, a positional subcommand form, not a flag) doesn't
// fit this shape -- add it here if that changes, rather than guess at a
// shape nothing exercises yet.
var resumeSpecs = map[string]resumeSpec{
	"claude":   {implicitFlags: []string{"--continue", "-c"}, explicitFlag: "--resume"},
	"opencode": {implicitFlags: []string{"--continue", "-c"}, explicitFlag: "--session"},
}

// PinResumeArgs rewrites command (command[0] is the wrapped CLI executable,
// the rest its argv) to explicitly resume sessionID via the adapter's own
// documented "resume by ID" flag, in place of whatever implicit
// "most-recent" flag was present. command is returned unchanged when
// sessionID is empty, the wrapped executable's basename isn't a known
// adapter, or the explicit resume flag is already present with a value --
// an operator's own explicit choice is never second-guessed.
func PinResumeArgs(command []string, sessionID string) []string {
	if len(command) == 0 || sessionID == "" {
		return command
	}
	spec, ok := resumeSpecs[filepath.Base(command[0])]
	if !ok {
		return command
	}
	for _, arg := range command[1:] {
		if arg == spec.explicitFlag {
			return command
		}
	}
	implicit := make(map[string]bool, len(spec.implicitFlags))
	for _, f := range spec.implicitFlags {
		implicit[f] = true
	}
	out := make([]string, 0, len(command)+2)
	out = append(out, command[0])
	for _, arg := range command[1:] {
		if implicit[arg] {
			continue
		}
		out = append(out, arg)
	}
	out = append(out, spec.explicitFlag, sessionID)
	return out
}
