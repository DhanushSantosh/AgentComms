//go:build windows

package interactiveserve

import (
	"context"
	"errors"
)

// Serve is not supported on Windows: it requires a real pty, and
// github.com/creack/pty (used by the unix implementation in serve.go) is
// unix-only. This is not a regression versus the tmux-backed design it
// replaces — tmux itself never worked on Windows either.
func Serve(ctx context.Context, opts ServeOptions) (int, error) {
	return 1, errors.New("interactiveserve: interactive-serve is not supported on Windows (requires a real pty); run codex/opencode directly instead")
}
