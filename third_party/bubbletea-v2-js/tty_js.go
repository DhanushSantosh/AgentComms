//go:build js

package tea

// Under js/wasm there is never a real terminal file descriptor — Program
// is always given a synthetic io.Reader/io.Writer (see the real
// tty_unix.go's initInput: its entire body is gated on p.input/p.output
// satisfying term.File, which a plain io.Reader/io.Writer never does).
// This is a faithful no-op for that already-existing behavior, not a
// reduced-functionality stub.
func (p *Program) initInput() error { return nil }
