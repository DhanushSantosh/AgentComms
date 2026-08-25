package cliui

import (
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const defaultWidth = 80

// TerminalContext is the raw environment used to resolve rendering policy.
// It is public so callers and tests can make capability decisions explicit.
type TerminalContext struct {
	Interactive bool
	Width       int
	Term        string
	Locale      string
	NoColor     bool
}

// CapabilitiesFor converts raw terminal facts into the presentation features
// the CLI may safely use.
func CapabilitiesFor(context TerminalContext) Capabilities {
	width := context.Width
	if width <= 0 {
		width = defaultWidth
	}
	interactive := context.Interactive && !strings.EqualFold(context.Term, "dumb")
	locale := strings.ToUpper(context.Locale)
	unicode := interactive && (strings.Contains(locale, "UTF-8") || strings.Contains(locale, "UTF8"))
	return Capabilities{
		Interactive: interactive,
		Color:       interactive && !context.NoColor,
		Unicode:     unicode,
		Width:       width,
	}
}

// DetectCapabilities resolves capabilities for a real output destination.
// Non-file writers and redirected files deterministically receive plain-safe
// capabilities.
func DetectCapabilities(output io.Writer, noColor bool) Capabilities {
	context := TerminalContext{
		Term:    os.Getenv("TERM"),
		Locale:  terminalLocale(),
		NoColor: noColor || os.Getenv("NO_COLOR") != "",
	}
	file, ok := output.(*os.File)
	if !ok {
		return CapabilitiesFor(context)
	}
	fd := int(file.Fd())
	context.Interactive = term.IsTerminal(fd)
	if context.Interactive {
		if width, _, err := term.GetSize(fd); err == nil {
			context.Width = width
		}
	}
	return CapabilitiesFor(context)
}

func terminalLocale() string {
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
