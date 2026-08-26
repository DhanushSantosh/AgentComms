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
	Interactive  bool
	Width        int
	Term         string
	Locale       string
	NoColor      bool
	ColorProfile ColorProfile
	Hyperlinks   bool
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
	profile := context.ColorProfile
	if profile == "" {
		switch {
		case strings.Contains(strings.ToLower(context.Term), "256color"):
			profile = ColorANSI256
		case interactive:
			profile = ColorANSI
		default:
			profile = ColorNone
		}
	}
	if !interactive || context.NoColor {
		profile = ColorNone
	}
	return Capabilities{
		Interactive:  interactive,
		Color:        interactive && !context.NoColor,
		ColorProfile: profile,
		Unicode:      unicode,
		Hyperlinks:   interactive && context.Hyperlinks,
		Width:        width,
	}
}

// DetectCapabilities resolves capabilities for a real output destination.
// Non-file writers and redirected files deterministically receive plain-safe
// capabilities.
func DetectCapabilities(output io.Writer, noColor bool) Capabilities {
	context := TerminalContext{
		Term:         os.Getenv("TERM"),
		Locale:       terminalLocale(),
		NoColor:      noColor || environmentPresent("NO_COLOR"),
		ColorProfile: detectedColorProfile(),
		Hyperlinks:   supportsHyperlinks(),
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

func environmentPresent(name string) bool {
	_, present := os.LookupEnv(name)
	return present
}

func detectedColorProfile() ColorProfile {
	colorTerm := strings.ToLower(os.Getenv("COLORTERM"))
	if strings.Contains(colorTerm, "truecolor") || strings.Contains(colorTerm, "24bit") {
		return ColorTrueColor
	}
	if strings.Contains(strings.ToLower(os.Getenv("TERM")), "256color") {
		return ColorANSI256
	}
	return ColorANSI
}

func supportsHyperlinks() bool {
	if os.Getenv("WT_SESSION") != "" || os.Getenv("VTE_VERSION") != "" {
		return true
	}
	switch strings.ToLower(os.Getenv("TERM_PROGRAM")) {
	case "iterm.app", "wezterm", "vscode", "ghostty":
		return true
	default:
		return false
	}
}

func terminalLocale() string {
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
