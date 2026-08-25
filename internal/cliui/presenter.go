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

// Document is the smallest semantic result rendered by Presenter. Richer
// result shapes build on this contract rather than printing backend objects.
type Document struct {
	Title  string
	Fields []Field
}

// Presenter owns terminal-facing result rendering. Serialization of the
// stable JSON envelope remains in internal/app.
type Presenter struct {
	Out  io.Writer
	Mode Mode
}

// Render writes a bounded semantic document in human or plain mode.
func (p Presenter) Render(document Document) error {
	if p.Out == nil {
		return fmt.Errorf("CLI presenter output is required")
	}
	if p.Mode != ModeHuman && p.Mode != ModePlain {
		return fmt.Errorf("CLI presenter cannot render mode %q", p.Mode)
	}
	if _, err := fmt.Fprintln(p.Out, safeText(document.Title)); err != nil {
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

func safeText(value string) string {
	value = ansi.Strip(value)
	return strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return -1
		}
		return character
	}, value)
}
