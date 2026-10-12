package interactiveserve

import (
	"io"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/vt"
)

// terminalScreen renders the wrapped child's output with a real VT emulator,
// so delivery decisions are made against what is actually on screen right
// now -- the way the original tmux-backed design read tmux's own rendered
// grid -- instead of against a tail of raw bytes.
//
// The raw-byte tee it replaces was wrong in ways no amount of pattern
// tweaking could fix, each confirmed with a scripted fake agent under a real
// ConPTY:
//
//   - Stale busy markers. A TUI that clears "esc to interrupt" by
//     overwriting it in place (differential rendering) leaves the old bytes in
//     the tail buffer, and an idle TUI emits almost nothing new to push them
//     out, so the session read as busy forever and every delivery was
//     refused while the screen plainly showed it idle.
//   - Stale echo matches. A previous delivery's text stays in the buffer, so
//     the loose fuzzy match accepted it as the echo of the NEXT message and
//     Enter could go out before the new text had been received at all.
//   - Escape-sequence soup. Cursor-addressed redraws, wraps and borders had
//     to be untangled by heuristics; an emulator resolves them by
//     construction.
//
// The emulator is fed from the same output copy that drives the user's real
// terminal, so it must never block or fail that copy: Write always reports
// success, recovers from an emulator panic by starting a fresh screen (a TUI
// repaints itself soon enough), and the emulator's own replies to terminal
// queries are drained and discarded -- the user's real terminal already
// answers those, and an unread reply pipe would block the emulator, and with
// it the session's output, forever.
type terminalScreen struct {
	mu        sync.Mutex
	emu       *vt.Emulator
	cols      int
	rows      int
	lastWrite time.Time
}

func newTerminalScreen(cols, rows int) *terminalScreen {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	s := &terminalScreen{cols: cols, rows: rows}
	s.emu = s.newEmulator()
	return s
}

func (s *terminalScreen) newEmulator() *vt.Emulator {
	emu := vt.NewEmulator(s.cols, s.rows)
	go func() { _, _ = io.Copy(io.Discard, emu) }()
	return emu
}

// retireEmulator stops an emulator's reply-drain goroutine. It closes the
// reply pipe rather than calling Emulator.Close: Close writes an unguarded
// "closed" flag that Emulator.Read checks, which the race detector caught
// against the drain goroutine. The pipe itself is safe for concurrent use,
// and closing its writer makes the pending Read return EOF.
func retireEmulator(emu *vt.Emulator) {
	if closer, ok := emu.InputPipe().(io.Closer); ok {
		_ = closer.Close()
	}
}

func (s *terminalScreen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastWrite = time.Now()
	func() {
		defer func() {
			if recover() != nil {
				retireEmulator(s.emu)
				s.emu = s.newEmulator()
			}
		}()
		_, _ = s.emu.Write(p)
	}()
	return len(p), nil
}

// Resize keeps the emulator's geometry in step with the child's pty, which
// is what makes wrapped text land on the same rows it does on the user's
// screen.
func (s *terminalScreen) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cols == s.cols && rows == s.rows {
		return
	}
	s.cols, s.rows = cols, rows
	func() {
		defer func() {
			if recover() != nil {
				retireEmulator(s.emu)
				s.emu = s.newEmulator()
			}
		}()
		s.emu.Resize(cols, rows)
	}()
}

func (s *terminalScreen) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	retireEmulator(s.emu)
}

// sinceLastWrite reports how long the child has produced no output at all.
func (s *terminalScreen) sinceLastWrite() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastWrite.IsZero() {
		return time.Duration(1<<62 - 1)
	}
	return time.Since(s.lastWrite)
}

// screenSnapshot is the visible screen as plain text, one entry per row
// (trailing spaces trimmed), plus the cursor position.
type screenSnapshot struct {
	rows    []string
	cursorX int
	cursorY int
}

func (s *terminalScreen) snapshot() screenSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := screenSnapshot{rows: make([]string, s.rows)}
	var row strings.Builder
	for y := 0; y < s.rows; y++ {
		row.Reset()
		for x := 0; x < s.cols; {
			cell := s.emu.CellAt(x, y)
			width := 1
			if cell == nil || cell.Content == "" {
				row.WriteByte(' ')
			} else {
				row.WriteString(cell.Content)
				if cell.Width > 1 {
					width = cell.Width
				}
			}
			x += width
		}
		snap.rows[y] = strings.TrimRight(row.String(), " ")
	}
	position := s.emu.CursorPosition()
	snap.cursorX, snap.cursorY = position.X, position.Y
	return snap
}

// text renders the snapshot for humans (the TUI's live preview): rows joined
// by newlines, with trailing blank rows dropped.
func (snap screenSnapshot) text() string {
	end := len(snap.rows)
	for end > 0 && snap.rows[end-1] == "" {
		end--
	}
	return strings.Join(snap.rows[:end], "\n")
}
