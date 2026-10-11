package interactiveserve

import (
	"strings"
	"unicode"
)

// busyMarkers are substrings codex's, Claude Code's and opencode's TUIs were
// observed, live, to show in their status/footer line while actively working
// a turn (streaming a response, running a tool call) and to omit once idle
// and waiting for input again. This is a heuristic proxy, not a
// protocol-level signal -- none of them exposes one.
var busyMarkers = []string{"esc to interrupt", "esc again to interrupt", "esc interrupt"}

// busyRegionRows bounds how much of the screen screenBusy inspects: the
// bottom-most non-blank rows, where every supported TUI keeps its status
// and footer lines. Looking at the whole screen would read a transcript
// that merely quotes a marker (this very package's source, say) as busy.
const busyRegionRows = 12

// screenBusy reports whether the visible screen's status area currently
// shows a busy marker. It reads the rendered screen, not raw output, so a
// marker the TUI has already overwritten no longer counts.
func screenBusy(snap screenSnapshot) bool {
	seen := 0
	for y := len(snap.rows) - 1; y >= 0 && seen < busyRegionRows; y-- {
		row := snap.rows[y]
		if strings.TrimSpace(row) == "" {
			continue
		}
		seen++
		lower := strings.ToLower(row)
		for _, marker := range busyMarkers {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	return false
}

// normalizeForMatch keeps only letters and digits, lower-cased, so text
// compares equal across wrapping, box-drawing borders, prompt glyphs and
// punctuation the TUI adds or reflows.
func normalizeForMatch(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// normalizedScreen concatenates every row's normalized text, so a message
// the TUI wrapped across several rows reads as one contiguous run.
func normalizedScreen(snap screenSnapshot) string {
	var b strings.Builder
	for _, row := range snap.rows {
		b.WriteString(normalizeForMatch(row))
	}
	return b.String()
}

// minEchoNeedle is the shortest normalized fragment accepted as echo
// evidence on its own. Delivery only types while the target is idle, so
// nothing else is rendering that could produce a chance match; six
// characters still covers short custom invocation IDs ("inv-047").
const minEchoNeedle = 6

// echoTailLength is how much of a message's normalized tail counts as a
// fragment: enough to be distinctive, short enough to survive a composer
// that only shows the end of a long line or a status row wedged between
// wrapped segments.
const echoTailLength = 16

// newlyEchoed reports whether message (or, for TUIs that truncate, wrap
// around side panels, or collapse long input, a distinctive fragment of it)
// appears on screen MORE times now than it did before the text was typed.
//
// Requiring an increase, not mere presence, is the point: a previous
// delivery's text routinely stays visible in the transcript, and the old
// matcher accepted that stale copy as the echo of the next message. The
// fragments are the delivery's echo token (the invocation ID, which only
// this delivery introduces) and the message's normalized tail.
func newlyEchoed(before, after screenSnapshot, message, token string) bool {
	beforeText, afterText := normalizedScreen(before), normalizedScreen(after)
	needles := []string{normalizeForMatch(message), normalizeForMatch(token)}
	full := needles[0]
	if len(full) > echoTailLength {
		needles = append(needles, full[len(full)-echoTailLength:])
	}
	needles = append(needles, pastePlaceholders...)
	for i, needle := range needles {
		if needle == "" || (i > 0 && len(needle) < minEchoNeedle) {
			continue
		}
		if strings.Count(afterText, needle) > strings.Count(beforeText, needle) {
			return true
		}
	}
	return false
}

// newlyShownComplete reports whether the complete message appears on screen
// more times than before. Used after Enter, where it is evidence that a
// line-oriented program read the line and printed it back. Fragments are
// not enough there: a TUI re-laying out its composer can duplicate a tail.
func newlyShownComplete(before, after screenSnapshot, message string) bool {
	full := normalizeForMatch(message)
	return full != "" && strings.Count(normalizedScreen(after), full) > strings.Count(normalizedScreen(before), full)
}

// pastePlaceholders are what TUIs render instead of long pasted input
// (codex: "[Pasted Content 1234 chars]", Claude Code: "[Pasted text #1 +3
// lines]"), normalized. A newly appearing placeholder proves the input
// arrived even though its text is not on screen.
var pastePlaceholders = []string{"pastedcontent", "pastedtext"}

// composerRows is how many contiguous rows above the cursor are read as one
// composer: enough for a wrapped notice on a narrow terminal.
const composerRows = 16

// cursorRowHoldsMessage reports whether message is still sitting, complete,
// in the composer at the cursor after Enter was sent. The composer is read
// as the text up to the cursor on its row plus the contiguous non-blank rows
// above it, so a message wrapped across several rows still matches; the
// cursor's own row may be blank, which is exactly what codex shows when it
// takes an Enter as a newline inside a paste burst. A TUI that keeps its cursor in
// the input box (codex does) clears it on a real submit; one that parks the
// cursor elsewhere never matches, which only means no second Enter is sent.
func cursorRowHoldsMessage(snap screenSnapshot, message string) bool {
	holds, _ := composerHoldsMessage(snap, message)
	return holds
}

// composerHoldsMessage is cursorRowHoldsMessage that also reports whether
// the cursor sits on an empty line below the message -- the Enter was taken
// as a newline, which a backspace removes before Enter is sent again.
//
// The composer must end with the COMPLETE message, not just its tail. Found
// by the fake-agent suite: a TUI that left stale fragments of the wrapped
// message above an emptied composer read as "still unsent" under a
// tail-only check, which pressed Enter on an empty composer, gave up, and
// had the delivery retried -- a duplicate wake-up. Fragments cannot match a
// complete message; a genuinely swallowed Enter always leaves one.
func composerHoldsMessage(snap screenSnapshot, message string) (holds, newlineInserted bool) {
	if snap.cursorY < 0 || snap.cursorY >= len(snap.rows) {
		return false, false
	}
	tail := normalizeForMatch(message)
	if len(tail) < 4 {
		return false, false
	}
	row := []rune(snap.rows[snap.cursorY])
	if snap.cursorX >= 0 && snap.cursorX < len(row) {
		row = row[:snap.cursorX]
	}
	cursorText := normalizeForMatch(string(row))
	composer := cursorText
	for y := snap.cursorY - 1; y >= 0 && y > snap.cursorY-composerRows; y-- {
		if strings.TrimSpace(snap.rows[y]) == "" {
			break
		}
		composer = normalizeForMatch(snap.rows[y]) + composer
	}
	if !strings.HasSuffix(composer, tail) {
		return false, false
	}
	return true, cursorText == ""
}
