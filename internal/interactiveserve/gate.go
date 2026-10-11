package interactiveserve

import (
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
// paste is.
type inputGate struct {
	mu     sync.Mutex // held for the duration of a delivery
	target writeFlusher

	stateMu   sync.Mutex
	lastTyped time.Time
	draft     int // printable characters typed since the last submit/clear
	parse     inputParseState
}

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
					switch string(st.seq) {
					case "[200~":
						st.inPaste = true
					case "[201~":
						st.inPaste = false
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
		switch {
		case b == 0x1b:
			st.inEscape, st.seq = true, st.seq[:0]
		case b == '\r' || b == '\n':
			if st.inPaste {
				g.draft++
				typed = true
			} else {
				g.draft = 0
			}
		case b == 0x03 || b == 0x15: // Ctrl-C, Ctrl-U: composer cleared
			g.draft = 0
		case b == 0x7f || b == 0x08: // backspace
			if g.draft > 0 {
				g.draft--
			}
			typed = true
		case b >= 0x20 || b == '\t':
			if b < 0x80 || b >= 0xc0 { // count UTF-8 lead bytes only
				g.draft++
			}
			typed = true
		}
	}
	if typed {
		g.lastTyped = time.Now()
	}
}
