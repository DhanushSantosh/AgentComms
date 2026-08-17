//go:build js

package tea

// suspendSupported gates SuspendMsg handling (tea.go) — there is no
// process-suspend concept in a browser sandbox.
const suspendSupported = false

// Never actually called: reachable only when suspendSupported is true.
func suspendProcess() {}

// listenForResize is only invoked when p.ttyOutput != nil (tea.go's
// handleResize), which never happens for non-tty output. This project's
// real resize path is a synthetic event encoded into the input byte
// stream, handled elsewhere, not through this OS-signal mechanism.
func (p *Program) listenForResize(done chan struct{}) { close(done) }
