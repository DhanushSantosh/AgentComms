// Package interactiveserve lets one running agent (Codex or OpenCode) directly
// wake another's already-open interactive terminal session and deliver a
// notification about a pending signed invocation — so two runtimes can hold
// a real, live, agent-to-agent conversation visible in their own real
// terminal UIs, in any terminal emulator, not just a specific multiplexer.
//
// AgentComms owns the pty directly rather than relying on an external
// multiplexer: `agent-comms runtime interactive-serve --id <runtimeID> --
// <command>` allocates its own pty, execs the real provider CLI attached to
// it, and transparently forwards the wrapper's own stdin/stdout so the
// terminal emulator shows the child's native UI unmediated (see serve.go).
// The owning process listens on a control socket at a path deterministic in
// (project root, runtime ID) — see SocketPath — so "is this runtime live"
// is simply "can I dial its socket," with no separate registry file to keep
// in sync, and concurrent deliveries from multiple senders naturally
// serialize through the one process that owns the pty, with no cross-process
// lock required.
//
// Delivery is intentionally narrow: the injected text is always this
// package's own fixed notification template (see NotifyInvocation), never
// raw instruction content. The target runtime is expected to read the
// instruction back through the normal, audited agent-comms CLI
// (list/claim/start/complete), the same as every other adapter — this keeps
// terminal injection limited to "wake up and look," never a second,
// unaudited channel for instruction content to reach a runtime.
package interactiveserve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ServeOptions configures Serve. Defined here, with no build tag, so both
// the unix implementation (serve.go) and the Windows stub (serve_windows.go)
// can reference the same type without duplicating it.
type ServeOptions struct {
	ProjectRoot string
	RuntimeID   string
	Command     []string

	// ControlFD is the file descriptor of the wrapper's own controlling
	// terminal — the one put into raw mode and resized on SIGWINCH. Defaults
	// to os.Stdin.Fd() in the real CLI path; overridable so tests can point
	// it at a synthetic pty instead of the test process's own terminal.
	ControlFD int
	// Stdin/Stdout default to os.Stdin/os.Stdout; overridable for the same
	// testing reason as ControlFD.
	Stdin  io.Reader
	Stdout io.Writer
}

// Serve allocates a real pty, execs Command attached to it, and transparently
// forwards ControlFD/Stdin/Stdout so the invoking terminal shows Command's
// native UI unmediated, while also listening on this runtime's control
// socket so other processes can wake it with Deliver. It returns the child's
// exit code and never calls os.Exit itself — the caller must do that only
// after Serve has returned, so pending cleanup (terminal-mode restore, socket
// removal) always runs first. Implemented in serve.go (unix); see
// serve_windows.go for why this is unix-only.

// maxUnixSocketPathLen is a conservative safety margin under the real,
// hard OS limits on AF_UNIX socket paths — 108 bytes total on Linux
// (sockaddr_un.sun_path, including the null terminator) and 104 on macOS/
// BSD. Confirmed live: a path nested under a project root plus
// ".agent-comms/cache/interactive-sockets/<runtimeID>.sock" reliably blows
// through this for realistic project paths, not just unusually deep test
// temp dirs — this is a real constraint to design around, not a rare edge
// case.
const maxUnixSocketPathLen = 100

// SocketPath returns the deterministic control-socket path for runtimeID
// within a project. There is no separate registry file: "is a runtime live"
// is simply "can I dial this path." Deliberately does NOT nest under
// projectRoot itself (unlike every other local-routing-metadata file in this
// project, e.g. sessionbind's bindings file) — a unix domain socket path has
// a hard OS length limit far shorter than an ordinary file path, so this
// hashes projectRoot into a short, deterministic name under the OS temp
// directory instead. The runtime ID is kept human-readable in the common
// case; if it's unusually long enough to still risk the limit, it's hashed
// too rather than truncated, to avoid two different long IDs silently
// colliding on a shared prefix.
func SocketPath(projectRoot, runtimeID string) string {
	projectHash := sha256.Sum256([]byte(projectRoot))
	name := fmt.Sprintf("%s-%s.sock", hex.EncodeToString(projectHash[:4]), runtimeID)
	path := filepath.Join(os.TempDir(), "agent-comms-interactive", name)
	if len(path) <= maxUnixSocketPathLen {
		return path
	}
	runtimeHash := sha256.Sum256([]byte(runtimeID))
	name = fmt.Sprintf("%s-%s.sock", hex.EncodeToString(projectHash[:4]), hex.EncodeToString(runtimeHash[:8]))
	return filepath.Join(os.TempDir(), "agent-comms-interactive", name)
}

// Alive reports whether runtimeID currently has a live interactive-serve
// process dialable at its deterministic socket.
func Alive(ctx context.Context, projectRoot, runtimeID string) bool {
	resp, err := call(ctx, SocketPath(projectRoot, runtimeID), Request{Kind: "ping"})
	return err == nil && resp.OK
}

// Deliver asks runtimeID's owning interactive-serve process to inject
// message as terminal input, waiting for its busy/echo-gated delivery to
// finish or fail (see serve.go's deliver for that sequencing). message must
// be a single line: Serve sends exactly one line of input followed by one
// Enter keystroke, not a multi-line paste.
func Deliver(ctx context.Context, projectRoot, runtimeID, message string) error {
	if strings.ContainsAny(message, "\n\r") {
		return errors.New("interactiveserve: message must be a single line; Deliver sends one line of terminal input followed by one Enter, not a multi-line paste")
	}
	resp, err := call(ctx, SocketPath(projectRoot, runtimeID), Request{Kind: "deliver", Message: message})
	if err != nil {
		return fmt.Errorf("interactiveserve: deliver to %q: %w", runtimeID, err)
	}
	if !resp.OK {
		return fmt.Errorf("interactiveserve: %q refused delivery: %s", runtimeID, resp.Error)
	}
	return nil
}

// NotifyInvocation delivers a standard, bounded notification for a newly
// requested invocation — not the invocation's instruction itself, since the
// runtime is expected to read that back through the normal, auditable
// agent-comms CLI (list/claim/start/complete), the same as every other
// adapter's invocation handling. This keeps the terminal-injection channel
// limited to "wake up and look," never a second, unaudited path for
// instruction content to reach the runtime.
func NotifyInvocation(ctx context.Context, projectRoot, targetRuntimeID, invocationID, requestedBy string) error {
	message := fmt.Sprintf(
		"Agent Comms: new invocation %q is pending for you (requested by %s). Run agent-comms invocation list --status PENDING --to %s --json to see it, then handle it per your existing protocol.",
		invocationID, requestedBy, targetRuntimeID,
	)
	return Deliver(ctx, projectRoot, targetRuntimeID, message)
}
