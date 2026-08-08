// Package terminallaunch opens a new, real terminal-emulator window running
// a given command in a given working directory, for callers (like
// interactive-serve's --launch-terminal flag) that need a genuinely
// separate, dedicated terminal without asking the operator to open one and
// type the command themselves.
//
// This is deliberately best-effort: there is no portable "open a terminal"
// primitive, so each platform tries a short, ordered list of known terminal
// programs and reports a clear error naming everything it tried if none are
// available, rather than silently doing nothing.
package terminallaunch
