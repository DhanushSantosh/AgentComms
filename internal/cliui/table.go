package cliui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Table is a copy-friendly, display-width-aware collection view.
type Table struct {
	Headers []string
	Rows    [][]string
}

// RenderTable writes an aligned table without borders so it remains useful
// when copied, redirected, or pasted into another command.
func (p Presenter) RenderTable(table Table) error {
	if p.Out == nil {
		return fmt.Errorf("CLI presenter output is required")
	}
	if p.Mode != ModeHuman && p.Mode != ModePlain {
		return fmt.Errorf("CLI presenter cannot render mode %q", p.Mode)
	}
	if len(table.Rows) == 0 {
		_, err := fmt.Fprintln(p.Out, "(no rows)")
		return err
	}
	headers := sanitizeCells(table.Headers)
	rows := make([][]string, len(table.Rows))
	for index, row := range table.Rows {
		rows[index] = sanitizeCells(row)
	}
	widths := make([]int, len(headers))
	for index, header := range headers {
		widths[index] = ansi.StringWidth(header)
	}
	for _, row := range rows {
		for index, cell := range row {
			if index < len(widths) && ansi.StringWidth(cell) > widths[index] {
				widths[index] = ansi.StringWidth(cell)
			}
		}
	}
	writeRow := func(cells []string) error {
		parts := make([]string, len(headers))
		for index := range headers {
			cell := ""
			if index < len(cells) {
				cell = cells[index]
			}
			parts[index] = cell + strings.Repeat(" ", widths[index]-ansi.StringWidth(cell))
		}
		_, err := fmt.Fprintln(p.Out, strings.TrimRight(strings.Join(parts, "  "), " "))
		return err
	}
	if err := writeRow(headers); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeRow(row); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeCells(cells []string) []string {
	result := make([]string, len(cells))
	for index, cell := range cells {
		result[index] = safeText(cell)
	}
	return result
}
