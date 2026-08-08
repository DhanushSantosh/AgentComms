//go:build darwin

package terminallaunch

import (
	"fmt"
	"os/exec"
	"strings"
)

// shellQuote wraps s in single quotes for safe embedding as one POSIX
// shell word, escaping any single quote it already contains.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildScript constructs the osascript AppleScript source that tells
// Terminal.app to open a new window running argv with dir as its working
// directory. Exported as its own function so the string-building logic is
// testable without actually launching Terminal.app.
func buildScript(dir string, argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	shellCmd := fmt.Sprintf("cd %s && exec %s", shellQuote(dir), strings.Join(quoted, " "))
	// Escape for embedding inside the AppleScript double-quoted string
	// literal below -- backslash and double-quote are AppleScript's own
	// escape-sensitive characters.
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(shellCmd)
	return fmt.Sprintf(`tell application "Terminal" to do script "%s"`, escaped)
}

// Open launches argv inside a new Terminal.app window rooted at dir. It
// does not wait for the window to exit -- the caller's own process is
// expected to return immediately, leaving the new window as the dedicated
// session.
func Open(dir string, argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("no command to launch")
	}
	cmd := exec.Command("osascript", "-e", buildScript(dir, argv), "-e", `tell application "Terminal" to activate`)
	return cmd.Start()
}
