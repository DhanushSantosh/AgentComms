package cliui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Table is a copy-friendly, display-width-aware collection view.
type Table struct {
	Headers    []string
	Priorities []int
	Rows       [][]string
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
	active := make([]bool, len(headers))
	for index := range active {
		active[index] = true
	}
	totalWidth := func() int {
		width, columns := 0, 0
		for index, enabled := range active {
			if enabled {
				width += widths[index]
				columns++
			}
		}
		if columns > 1 {
			width += (columns - 1) * 2
		}
		return width
	}
	for p.Capabilities.Width > 0 && totalWidth() > p.Capabilities.Width {
		remove, score, remaining := -1, -1, 0
		for index, enabled := range active {
			if !enabled {
				continue
			}
			remaining++
			priority := index
			if index < len(table.Priorities) {
				priority = table.Priorities[index]
			}
			if priority >= score {
				remove, score = index, priority
			}
		}
		if remaining <= 1 || remove < 0 {
			break
		}
		active[remove] = false
	}
	writeRow := func(cells []string) error {
		parts := make([]string, 0, len(headers))
		for index := range headers {
			if !active[index] {
				continue
			}
			cell := ""
			if index < len(cells) {
				cell = cells[index]
			}
			parts = append(parts, cell+strings.Repeat(" ", widths[index]-ansi.StringWidth(cell)))
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
