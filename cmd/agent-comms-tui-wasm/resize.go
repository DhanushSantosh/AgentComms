package main

import "fmt"

// encodeWindowSizeEvent encodes a synthetic terminal resize as the exact byte
// sequence github.com/charmbracelet/ultraviolet's input decoder recognizes as
// a WindowSizeEvent.
//
// Confirmed by reading ultraviolet@v0.0.0-20260703014108-f5a850f9c2b7's
// decoder.go (func (p *EventDecoder) parseCsi, the `case 't':` branch,
// `case 8:` sub-case): a CSI sequence with exactly three semicolon-separated
// parameters -- "8", then height, then width -- terminated by 't':
//
//	CSI 8 ; height ; width t
//
// i.e. "\x1b[8;<rows>;<cols>t". This is xterm's standard "report window size
// in characters" reply (param 8 of the window manipulation CSI...t family;
// params 4 and 6 report pixel size and cell-pixel size respectively and are
// NOT what we want here). ultraviolet decodes this into exactly
// uv.WindowSizeEvent{Width: width, Height: height}, which
// bubbletea/v2's Program.translateInputEvent (screen.go) maps 1:1 onto
// tea.WindowSizeMsg{Width, Height} -- see decoder_test.go / resize_test.go
// in this package for a round-trip proof against the real parser.
func encodeWindowSizeEvent(cols, rows int) []byte {
	return []byte(fmt.Sprintf("\x1b[8;%d;%dt", rows, cols))
}
