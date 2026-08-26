package cliui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Mode selects the presentation contract used for a command result.
type Mode string

const (
	ModeHuman Mode = "human"
	ModePlain Mode = "plain"
	ModeJSON  Mode = "json"
	ModeJSONL Mode = "jsonl"
)

// Field is one labeled value in a human-readable result summary.
type Field struct {
	Label string
	Value string
}

// Status gives a result semantic meaning without coupling commands to colors
// or terminal glyphs.
type Status string

const (
	StatusNone    Status = ""
	StatusSuccess Status = "success"
	StatusInfo    Status = "info"
	StatusWarning Status = "warning"
	StatusDanger  Status = "danger"
)

// Capabilities describes presentation features supported by the destination.
// Commands do not infer these themselves, which keeps rendering deterministic
// in tests and consistent across the CLI.
type Capabilities struct {
	Interactive bool
	Color       bool
	Unicode     bool
	Width       int
}

// Document is the smallest semantic result rendered by Presenter. Richer
// result shapes build on this contract rather than printing backend objects.
type Document struct {
	Title  string
	Status Status
	Fields []Field
}

// Presenter owns terminal-facing result rendering. Serialization of the
// stable JSON envelope remains in internal/app.
type Presenter struct {
	Out          io.Writer
	Mode         Mode
	Capabilities Capabilities
}

// Render writes a bounded semantic document in human or plain mode.
func (p Presenter) Render(document Document) error {
	if p.Out == nil {
		return fmt.Errorf("CLI presenter output is required")
	}
	if p.Mode != ModeHuman && p.Mode != ModePlain {
		return fmt.Errorf("CLI presenter cannot render mode %q", p.Mode)
	}
	if err := p.renderTitle(document); err != nil {
		return err
	}
	if len(document.Fields) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(p.Out); err != nil {
		return err
	}
	width := 0
	for _, field := range document.Fields {
		label := safeText(field.Label)
		if len(label) > width {
			width = len(label)
		}
	}
	for _, field := range document.Fields {
		label := safeText(field.Label)
		label += strings.Repeat(" ", width-len(label))
		if _, err := fmt.Fprintf(p.Out, "%s  %s\n", label, safeText(field.Value)); err != nil {
			return err
		}
	}
	return nil
}

func (p Presenter) renderTitle(document Document) error {
	if p.Out == nil {
		return fmt.Errorf("CLI presenter output is required")
	}
	if p.Mode != ModeHuman && p.Mode != ModePlain {
		return fmt.Errorf("CLI presenter cannot render mode %q", p.Mode)
	}
	title := safeText(document.Title)
	if p.Mode == ModeHuman && p.Capabilities.Interactive {
		title = statusPrefix(document.Status, p.Capabilities.Unicode) + title
		if p.Capabilities.Color {
			title = statusStyle(document.Status) + "\x1b[1m" + title + "\x1b[0m"
		}
	}
	if _, err := fmt.Fprintln(p.Out, title); err != nil {
		return err
	}
	return nil
}

func statusPrefix(status Status, unicode bool) string {
	if status == StatusNone {
		return ""
	}
	if !unicode {
		switch status {
		case StatusSuccess:
			return "[ok] "
		case StatusWarning:
			return "[!] "
		case StatusDanger:
			return "[x] "
		default:
			return "[i] "
		}
	}
	switch status {
	case StatusSuccess:
		return "✓ "
	case StatusWarning:
		return "▲ "
	case StatusDanger:
		return "✕ "
	default:
		return "◆ "
	}
}

func statusStyle(status Status) string {
	switch status {
	case StatusSuccess:
		return "\x1b[32m"
	case StatusWarning:
		return "\x1b[33m"
	case StatusDanger:
		return "\x1b[31m"
	default:
		return "\x1b[36m"
	}
}

func safeText(value string) string {
	value = ansi.Strip(value)
	return strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return -1
		}
		return character
	}, value)
}
