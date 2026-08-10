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
	"strings"
	"time"
)

// ServeOptions configures Serve. Defined here, with no build tag, so both
// the unix implementation (serve.go) and the Windows stub (serve_windows.go)
// can reference the same type without duplicating it.
type ServeOptions struct {
	ProjectRoot string
	RuntimeID   string
	Command     []string

	// Actor is the identity the wrapper itself resolved (--actor, --profile,
	// host-label match, or the active profile) for its OWN agent-comms
	// actions. Serve exports it into the child's environment as
	// AGENT_COMMS_ACTOR, so the wrapped provider's own subsequent
	// agent-comms calls (invocation claim/start/complete, etc.) authenticate
	// as the same identity, instead of falling back to ambient owner
	// resolution -- the same fallback path that let an unregistered agent
	// self-grant ORCHESTRATOR before the elevated key existed. Left empty,
	// the child simply inherits the wrapper's environment unchanged and
	// resolves its own actor however it otherwise would.
	Actor string

	// ControlFD is the file descriptor of the wrapper's own controlling
	// terminal — the one put into raw mode and resized on SIGWINCH. Defaults
	// to os.Stdin.Fd() in the real CLI path; overridable so tests can point
	// it at a synthetic pty instead of the test process's own terminal.
	ControlFD int
	// Stdin/Stdout default to os.Stdin/os.Stdout; overridable for the same
	// testing reason as ControlFD.
	Stdin  io.Reader
	Stdout io.Writer

	// OnStarted, if set, is called once in its own goroutine right after
	// the child process starts, with its PID -- never on Serve's own
	// critical path, so a slow or blocking callback (e.g. one that polls
	// the filesystem for a session file the child hasn't written yet)
	// never delays the pty forwarding a human or another agent is waiting
	// to see. Intended for auto-discovering and persisting the child's own
	// provider-native session ID (see PinResumeArgs and sessionbind), so a
	// later --takeover-pid respawn of the same runtime ID can resume this
	// exact conversation by ID instead of falling back to the wrapped
	// CLI's own racy "most recent in this directory" behavior.
	OnStarted func(pid int)
}

// Serve allocates a real pty, execs Command attached to it, and transparently
// forwards ControlFD/Stdin/Stdout so the invoking terminal shows Command's
// native UI unmediated, while also listening on this runtime's control
// socket so other processes can wake it with Deliver. It returns the child's
// exit code and never calls os.Exit itself — the caller must do that only
// after Serve has returned, so pending cleanup (terminal-mode restore, socket
// removal) always runs first. Implemented in serve.go (unix); see
// serve_windows.go for why this is unix-only.

// SocketPath returns the deterministic control-socket address for runtimeID
// within a project — a unix domain socket path on unix, a named pipe
// address (`\\.\pipe\...`) on Windows (see controlAddress in
// socket_address_unix.go/socket_address_windows.go). There is no separate
// registry file: "is a runtime live" is simply "can I dial this address."
// The runtime ID is kept human-readable only when it is a safe filename/
// pipe-name component; unsafe or long values are hashed rather than
// truncated, avoiding traversal and prefix collisions.
func SocketPath(projectRoot, runtimeID string) string {
	projectHash := sha256.Sum256([]byte(projectRoot))
	runtimeHash := sha256.Sum256([]byte(runtimeID))
	runtimeComponent := safeRuntimeComponent(runtimeID)
	return controlAddress(projectHash, runtimeHash, runtimeComponent)
}

func safeRuntimeComponent(runtimeID string) string {
	if runtimeID != "" && runtimeID != "." && runtimeID != ".." && len(runtimeID) <= 48 {
		safe := true
		for _, character := range runtimeID {
			if character >= 'a' && character <= 'z' ||
				character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' ||
				character == '-' || character == '_' || character == '.' {
				continue
			}
			safe = false
			break
		}
		if safe {
			return runtimeID
		}
	}
	runtimeHash := sha256.Sum256([]byte(runtimeID))
	return hex.EncodeToString(runtimeHash[:8])
}

// Probe reports whether runtimeID currently has a live interactive-serve
// process and whether that session is busy. Callers can use the busy signal
// to avoid starting a delivery that would otherwise wait for the target's
// current turn to finish.
func Probe(ctx context.Context, projectRoot, runtimeID string) (alive, busy bool) {
	resp, err := call(ctx, SocketPath(projectRoot, runtimeID), Request{Kind: "ping"})
	return err == nil && resp.OK, err == nil && resp.OK && resp.Busy
}

// Alive reports whether runtimeID currently has a live interactive-serve
// process dialable at its deterministic socket.
func Alive(ctx context.Context, projectRoot, runtimeID string) bool {
	alive, _ := Probe(ctx, projectRoot, runtimeID)
	return alive
}

// Snapshot queries the runtime's control socket and returns a string snapshot
// of its live PTY output buffer.
func Snapshot(ctx context.Context, projectRoot, runtimeID string) (string, error) {
	resp, err := call(ctx, SocketPath(projectRoot, runtimeID), Request{Kind: "snapshot"})
	if err != nil {
		return "", fmt.Errorf("interactiveserve: snapshot %q: %w", runtimeID, err)
	}
	if !resp.OK {
		if resp.Error != "" {
			return "", errors.New(resp.Error)
		}
		return "", errors.New("interactiveserve: snapshot request failed")
	}
	return resp.OutputSnapshot, nil
}

// Deliver asks runtimeID's owning interactive-serve process to inject
// message as terminal input, waiting for its busy/echo-gated delivery to
// finish or fail (see serve.go's deliver for that sequencing). message must
// be a single line: Serve sends exactly one line of input followed by one
// Enter keystroke, not a multi-line paste.
func Deliver(ctx context.Context, projectRoot, runtimeID, message string) error {
	return deliver(ctx, projectRoot, runtimeID, "deliver", message)
}

// TryDeliver makes one short delivery attempt and refuses quickly when the
// target is busy or another delivery is already in progress.
func TryDeliver(ctx context.Context, projectRoot, runtimeID, message string) error {
	return deliver(ctx, projectRoot, runtimeID, "try-deliver", message)
}

type DeliveryReceipt struct {
	TextEchoedAt time.Time
	EnterSentAt  time.Time
}

func TryDeliverWithEvidence(ctx context.Context, projectRoot, runtimeID, message string) (DeliveryReceipt, error) {
	if strings.ContainsAny(message, "\n\r") {
		return DeliveryReceipt{}, errors.New("interactiveserve: message must be a single line")
	}
	resp, err := call(ctx, SocketPath(projectRoot, runtimeID), Request{Kind: "try-deliver", Message: message})
	if err != nil {
		return DeliveryReceipt{}, fmt.Errorf("interactiveserve: deliver to %q: %w", runtimeID, err)
	}
	if !resp.OK {
		return DeliveryReceipt{}, fmt.Errorf("interactiveserve: %q refused delivery: %s", runtimeID, resp.Error)
	}
	if resp.TextEchoedAt == nil || resp.EnterSentAt == nil ||
		resp.EnterSentAt.Before(*resp.TextEchoedAt) {
		return DeliveryReceipt{}, errors.New("interactiveserve: successful delivery omitted valid PTY evidence")
	}
	return DeliveryReceipt{TextEchoedAt: *resp.TextEchoedAt, EnterSentAt: *resp.EnterSentAt}, nil
}

func deliver(ctx context.Context, projectRoot, runtimeID, kind, message string) error {
	if strings.ContainsAny(message, "\n\r") {
		return errors.New("interactiveserve: message must be a single line; Deliver sends one line of terminal input followed by one Enter, not a multi-line paste")
	}
	resp, err := call(ctx, SocketPath(projectRoot, runtimeID), Request{Kind: kind, Message: message})
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
func NotifyInvocation(ctx context.Context, projectRoot, targetRuntimeID, targetAgentID, invocationID, requestedBy string) error {
	_, err := NotifyInvocationWithEvidence(ctx, projectRoot, targetRuntimeID, targetAgentID, invocationID, requestedBy)
	return err
}

func NotifyInvocationWithEvidence(ctx context.Context, projectRoot, targetRuntimeID, targetAgentID, invocationID, requestedBy string) (DeliveryReceipt, error) {
	message := fmt.Sprintf(
		"Agent Comms: new invocation %q is pending for you (requested by %s). Run agent-comms invocation list --status PENDING --to %s --json to see it, then handle it per your existing protocol.",
		invocationID, requestedBy, targetAgentID,
	)
	return TryDeliverWithEvidence(ctx, projectRoot, targetRuntimeID, message)
}
