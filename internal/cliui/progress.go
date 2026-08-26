package cliui

import (
	"fmt"
	"io"
)

// Progress is a small TTY-only lifecycle indicator. It never writes when
// output is redirected, quiet, plain, JSON, or JSONL.
type Progress struct {
	out     io.Writer
	enabled bool
	unicode bool
	active  bool
}

// NewProgress creates a progress lifecycle for stderr.
func NewProgress(out io.Writer, mode Mode, capabilities Capabilities, quiet bool) *Progress {
	return &Progress{
		out: out, enabled: !quiet && mode == ModeHuman && capabilities.Interactive,
		unicode: capabilities.Unicode,
	}
}

// Start displays a transient operation label.
func (progress *Progress) Start(label string) error {
	if !progress.enabled || progress.active {
		return nil
	}
	marker := "..."
	if progress.unicode {
		marker = "◌"
	}
	progress.active = true
	_, err := fmt.Fprintf(progress.out, "%s %s\r", marker, safeText(label))
	return err
}

// Stop clears the transient line and writes the final state. Calling Stop on
// disabled or already-stopped progress is safe.
func (progress *Progress) Stop(success bool, message string) error {
	if !progress.enabled || !progress.active {
		return nil
	}
	progress.active = false
	marker := "x"
	if success {
		marker = "ok"
	}
	if progress.unicode {
		marker = "✕"
		if success {
			marker = "✓"
		}
	}
	_, err := fmt.Fprintf(progress.out, "\r\x1b[2K%s %s\n", marker, safeText(message))
	return err
}
