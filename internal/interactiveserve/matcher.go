package interactiveserve

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// ansiEscapeRE matches ECMA-48 terminal escape sequences: CSI sequences
// (ESC [ params intermediates final-byte — cursor movement, colors, line/
// screen clears), OSC sequences (ESC ] ... terminated by BEL or ST — window
// title changes and similar), and simple two-byte escapes (ESC followed by
// a single control character). Confirmed live: a full-screen TUI (codex,
// opencode — both bubbletea-style) can redraw the same logical text across
// several separate writes with cursor-repositioning sequences interspersed
// between individual characters. Those sequences routinely contain digits
// (parameter bytes) and letters (the final command byte), which survive a
// plain "keep only letters and digits" filter and get treated as content —
// fragmenting what should be one contiguous run of real text into pieces
// with escape-sequence remnants spliced in between, breaking a substring
// match even though the real text is, in fact, present and correctly
// rendered. Stripping whole escape sequences first, before the alnum
// filter, is what a real terminal emulator effectively does when resolving
// "what's actually on screen" — tmux's own capture-pane did this for free
// in the earlier tmux-backed design; teeing the raw byte stream ourselves
// means we have to do it explicitly.
var ansiEscapeRE = regexp.MustCompile(`\x1b\][^\x07]*(?:\x07|\x1b\\)|\x1b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])`)

func stripANSI(s string) string {
	return ansiEscapeRE.ReplaceAllString(s, "")
}

// busyMarkers are substrings both codex's and opencode's TUIs were observed,
// live, to show in their status line while actively working a turn
// (streaming a response, running a tool call) and to omit once idle and
// waiting for input again. This is a heuristic proxy, not a protocol-level
// signal — neither provider exposes one, so this is the closest available.
var busyMarkers = []string{"esc to interrupt", "esc again to interrupt", "esc interrupt"}

// clearSequences are terminal escape sequences that indicate the visible
// screen was wiped or the alternate screen buffer was entered/exited. The
// tmux-backed design matched against tmux's own rendered screen grid, which
// already resolves clears and repaints for you; this design tees the raw
// byte stream instead, so without this, stale content from before a clear
// could still sit in the tail buffer and produce a false busy/echo match
// after the target has actually redrawn a fresh, unrelated screen.
var clearSequences = [][]byte{
	[]byte("\x1b[2J"),     // erase entire screen
	[]byte("\x1b[3J"),     // erase scrollback
	[]byte("\x1b[?1049h"), // enter alternate screen buffer
	[]byte("\x1b[?47h"),   // enter alternate screen buffer (older form)
}

// lastClearIndex returns the offset just past the last occurrence of any
// clearSequences within p, or -1 if none are present.
func lastClearIndex(p []byte) int {
	best := -1
	for _, seq := range clearSequences {
		if idx := bytes.LastIndex(p, seq); idx >= 0 {
			if end := idx + len(seq); end > best {
				best = end
			}
		}
	}
	return best
}

// outputTeeCap bounds how much raw output outputTee retains between clears —
// generous enough to hold several redraws of a status line and a reply, not
// so large that a long busy stretch with no clear at all grows unbounded.
const outputTeeCap = 8192

// outputTee is a bounded, concurrency-safe io.Writer that retains the most
// recent raw bytes written to it, resetting itself whenever it observes a
// clearSequences match — see the package-level comment on clearSequences for
// why that reset matters here in a way it never did for the tmux design.
type outputTee struct {
	mu  sync.Mutex
	buf []byte
}

func newOutputTee() *outputTee { return &outputTee{} }

func (t *outputTee) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if idx := lastClearIndex(p); idx >= 0 {
		t.buf = append([]byte(nil), p[idx:]...)
	} else {
		t.buf = append(t.buf, p...)
	}
	if len(t.buf) > outputTeeCap {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-outputTeeCap:]...)
	}
	return len(p), nil
}

// snapshot returns a copy of the currently retained bytes, safe to inspect
// without holding the tee's lock.
func (t *outputTee) snapshot() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]byte, len(t.buf))
	copy(out, t.buf)
	return out
}

// busyTailBytes bounds how much of the tee's tail isBusy inspects — a short,
// recent window, the same spirit as the tmux design's `capture-pane -S -6`
// (last few lines only), so a busy marker from long before the target's most
// recent redraw can't produce a stale positive.
const busyTailBytes = 2048

// isBusy reports whether buf's recent tail contains one of busyMarkers,
// case-insensitively.
func isBusy(buf []byte) bool {
	tail := buf
	if len(tail) > busyTailBytes {
		tail = tail[len(tail)-busyTailBytes:]
	}
	text := strings.ToLower(stripANSI(string(tail)))
	for _, marker := range busyMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// normalizeForMatch keeps only letters and digits, lowercased, discarding
// whitespace, punctuation, and box-drawing/border characters a TUI might
// render around wrapped text. Confirmed live: OpenCode's compose box renders
// a "┃" border character at the start of every wrapped line, which a long
// delivered message reliably wraps across in any reasonably-sized pane, so
// that border character ends up interleaved into captured output and breaks
// a literal substring match even though the message is, in fact, sitting
// right there, correctly delivered.
func normalizeForMatch(s string) string {
	s = stripANSI(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// echoed reports whether buf contains a normalized match for text — used to
// confirm delivered text was actually registered as input before pressing
// Enter, rather than trusting a fixed sleep to be long enough.
func echoed(buf []byte, text string) bool {
	normBuf := normalizeForMatch(string(buf))
	normText := normalizeForMatch(text)
	if strings.Contains(normBuf, normText) {
		return true
	}
	// Fallback: If text contains an invocation ID (inv-...), check if that unique ID is present in normBuf.
	// TUIs with line-wrap decorations (like agy/opencode) may break long text streams with UI status elements.
	if idx := strings.Index(text, "inv-"); idx >= 0 {
		end := idx + 4
		for end < len(text) && (text[end] >= '0' && text[end] <= '9' || text[end] >= 'a' && text[end] <= 'z' || text[end] >= 'A' && text[end] <= 'Z') {
			end++
		}
		invID := normalizeForMatch(text[idx:end])
		if len(invID) > 5 && strings.Contains(normBuf, invID) {
			return true
		}
	}
	return false
}
