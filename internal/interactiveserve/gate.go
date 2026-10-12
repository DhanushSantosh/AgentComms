package interactiveserve

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// inputGate sits between the human's keyboard (the wrapper's own stdin) and
// the child's pty input, which delivery also writes to. Without it the two
// writers interleave freely: keystrokes typed while a delivery is in flight
// land in the middle of the injected line, and a delivery that arrives while
// the human has a half-written prompt in the composer appends itself to that
// draft and submits both together.
//
// The gate does three things:
//
//   - Holding: a delivery takes the gate for its whole type/echo/Enter
//     sequence, so human input waits (it is not dropped -- the reader simply
//     blocks) and is forwarded, in order, once the delivery is done.
//   - Typing: it remembers when the human last typed, so a delivery never
//     starts while someone is mid-sentence.
//   - Drafts: it tracks whether the human has typed text since their last
//     submit (Enter) or line clear (Ctrl-C, Ctrl-U). A pending draft makes
//     the session busy for delivery purposes until it is submitted or
//     cleared: typing a wake-up line onto someone's unfinished prompt and
//     pressing Enter would send their half-written text along with it.
//
// Escape sequences (arrow keys, mouse reports, focus events, terminal query
// replies) are not typing and never create a draft; text inside a bracketed
// paste is. Windows "win32-input-mode" key events are decoded: a nested
// ConPTY asks its host for that mode at startup, interactive-serve forwards
// the request to the user's terminal like any other output, and Windows
// Terminal then encodes every key -- letters, Enter, backspace -- as
// "CSI Vk;Sc;Uc;Kd;Cs;Rc _". Confirmed with a raw stdin capture under
// ConPTY: without decoding, all typing was invisible to the gate, and a
// draft typed as plain text could never be cleared by an encoded Enter.
type inputGate struct {
	mu     sync.Mutex // held for the duration of a delivery
	target writeFlusher

	stateMu   sync.Mutex
	lastTyped time.Time
	draft     int    // printable characters typed since the last submit/clear
	draftTail []rune // the last draftTailLength of them, for checking against the screen
	parse     inputParseState
}

// draftTailLength is how many of the human's most recent draft characters
// are remembered to confirm, on screen, that the draft still exists.
const draftTailLength = 24

type inputParseState struct {
	inEscape  bool
	escapeCSI bool
	escapeOSC bool
	sawESC    bool
	inPaste   bool
	seq       []byte
}

func newInputGate(target writeFlusher) *inputGate {
	return &inputGate{target: target}
}

// Write forwards human input to the child, waiting for any in-flight
// delivery to finish first.
func (g *inputGate) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.observe(p)
	return g.target.Write(p)
}

// humanQuietFor reports whether the human has neither typed within d nor
// left an unsubmitted draft in the composer.
func (g *inputGate) humanQuietFor(d time.Duration) bool {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	return g.draft == 0 && (g.lastTyped.IsZero() || time.Since(g.lastTyped) >= d)
}

// humanQuietOnScreen is humanQuietFor, except that a draft the screen shows
// is gone no longer counts. The gate only models Enter, Ctrl-C, Ctrl-U and
// backspace; a human can also empty the composer with keys it cannot
// interpret (Esc, word-delete, a selection), and a draft that stayed
// "pending" forever would hold every delivery indefinitely. Once the human
// has paused, a remembered draft whose most recent characters are no longer
// in the composer at the cursor is dropped. A draft that cannot be checked
// (nothing but punctuation typed) stays pending: waiting is the safe side.
func (g *inputGate) humanQuietOnScreen(d time.Duration, snap screenSnapshot) bool {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	if !g.lastTyped.IsZero() && time.Since(g.lastTyped) < d {
		return false
	}
	if g.draft == 0 {
		return true
	}
	tail := normalizeForMatch(string(g.draftTail))
	if tail != "" && !strings.Contains(composerText(snap), tail) {
		g.draft, g.draftTail = 0, nil
		return true
	}
	return false
}

func (g *inputGate) observe(p []byte) {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	typed := false
	st := &g.parse
	for _, b := range p {
		if st.inEscape {
			st.seq = append(st.seq, b)
			switch {
			case st.escapeOSC:
				if b == 0x07 || (st.sawESC && b == '\\') {
					st.inEscape, st.escapeOSC = false, false
				}
				st.sawESC = b == 0x1b
			case st.escapeCSI:
				if b >= 0x40 && b <= 0x7e {
					switch seq := string(st.seq); {
					case seq == "[200~":
						st.inPaste = true
					case seq == "[201~":
						st.inPaste = false
					case b == '_':
						if r, repeat, ok := win32KeyDown(seq); ok {
							for i := 0; i < repeat; i++ {
								typed = g.key(r) || typed
							}
						}
					}
					st.inEscape, st.escapeCSI = false, false
				}
			case len(st.seq) == 1 && b == '[':
				st.escapeCSI = true
			case len(st.seq) == 1 && b == ']':
				st.escapeOSC = true
			case len(st.seq) == 1 && b == 'O':
				// SS3: one more byte follows (F1-F4, keypad).
			default:
				st.inEscape = false
			}
			if len(st.seq) > 64 {
				st.inEscape, st.escapeCSI, st.escapeOSC = false, false, false
			}
			continue
		}
		if b == 0x1b {
			st.inEscape, st.seq = true, st.seq[:0]
			continue
		}
		if b >= 0x80 && b < 0xc0 {
			continue // UTF-8 continuation byte: its lead byte already counted
		}
		typed = g.key(rune(b)) || typed
	}
	if typed {
		g.lastTyped = time.Now()
	}
}

// key applies one typed character to the draft state and reports whether it
// counts as the human typing. Called with stateMu held.
func (g *inputGate) key(r rune) bool {
	switch {
	case r == '\r' || r == '\n':
		if g.parse.inPaste {
			g.addDraft(' ')
			return true
		}
		g.draft, g.draftTail = 0, nil
		return false
	case r == 0x03 || r == 0x15: // Ctrl-C, Ctrl-U: composer cleared
		g.draft, g.draftTail = 0, nil
		return false
	case r == 0x7f || r == 0x08: // backspace
		if g.draft > 0 {
			g.draft--
		}
		if len(g.draftTail) > 0 {
			g.draftTail = g.draftTail[:len(g.draftTail)-1]
		}
		return true
	case r >= 0x20 || r == '\t':
		g.addDraft(r)
		return true
	}
	return false
}

func (g *inputGate) addDraft(r rune) {
	g.draft++
	g.draftTail = append(g.draftTail, r)
	if len(g.draftTail) > draftTailLength {
		g.draftTail = g.draftTail[len(g.draftTail)-draftTailLength:]
	}
}

// win32KeyDown decodes a win32-input-mode key event ("[Vk;Sc;Uc;Kd;Cs;Rc",
// the CSI body without its final '_') and returns its character and repeat
// count when it is a key press that produces one. Key releases and keys
// without a character (arrows, modifiers alone) report ok=false.
func win32KeyDown(seq string) (r rune, repeat int, ok bool) {
	if !strings.HasPrefix(seq, "[") || !strings.HasSuffix(seq, "_") {
		return 0, 0, false
	}
	fields := strings.Split(seq[1:len(seq)-1], ";")
	value := func(i, fallback int) int {
		if i >= len(fields) || fields[i] == "" {
			return fallback
		}
		n, err := strconv.Atoi(fields[i])
		if err != nil {
			return -1
		}
		return n
	}
	char, down, count := value(2, 0), value(3, 0), value(5, 1)
	if down != 1 || char <= 0 || count < 1 {
		return 0, 0, false
	}
	if count > 64 {
		count = 64
	}
	return rune(char), count, true
}
