//go:build js

package doctor

import "context"

// interactiveSocketProbe cannot dial anything in a js/wasm build: an
// interactive runtime's control socket is a unix socket owned by a real OS
// process attached to a real terminal, and a browser sandbox has none of
// those. It returns probed=false so the caller withholds the
// INTERACTIVE_SOCKET_UNAVAILABLE finding rather than reporting a socket
// failure it never actually observed.
func interactiveSocketProbe(ctx context.Context, projectRoot, runtimeID string) (alive, probed bool) {
	_, _, _ = ctx, projectRoot, runtimeID
	return false, false
}
