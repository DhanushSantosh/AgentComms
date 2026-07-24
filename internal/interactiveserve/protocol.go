package interactiveserve

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Request is the single message shape sent over a runtime's control socket.
// Newline-delimited JSON over the raw connection is deliberately simpler
// than the project's existing HTTP-over-unix-socket daemon control plane
// (internal/daemon) — that machinery earns its weight with many routes and a
// long-lived multi-tenant daemon; here it's one connection per delivery
// attempt and exactly two verbs.
type Request struct {
	Kind    string `json:"kind"`              // "ping" or "deliver"
	Message string `json:"message,omitempty"` // required for "deliver"
}

// Response is the single message shape returned for a Request.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Busy  bool   `json:"busy,omitempty"` // set on "ping" responses
}

const (
	// dialTimeout only needs to prove the socket is live, not wait out an
	// actual delivery.
	dialTimeout = 2 * time.Second
	// connDeadline bounds the whole request/response round trip. It sits
	// comfortably above idleTimeout+echoTimeout (defined in serve.go) so a
	// misbehaving or wedged server can never hang a caller forever.
	connDeadline = 130 * time.Second
)

// call dials socketPath, sends req, and returns the decoded response. Every
// exported client function in this package (Alive, Deliver) is a thin
// wrapper around this.
func call(ctx context.Context, socketPath string, req Request) (Response, error) {
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(connDeadline)); err != nil {
		return Response{}, err
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, fmt.Errorf("interactiveserve: send request: %w", err)
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("interactiveserve: read response: %w", err)
	}
	return resp, nil
}

// listenLocal binds sockPath, refusing to start if another live process
// already owns it and cleaning up a stale leftover from a crashed one.
// Mirrors the exact sequence internal/daemon/listener_unix.go's ListenLocal
// already proves out for this project's daemon control socket, kept local
// here rather than cross-imported since these are otherwise unrelated
// subsystems. Pure Go, nothing platform-specific — lives here (no build
// tag) rather than in serve.go so it stays testable on every platform even
// though only the unix-only Serve ever actually calls it in production.
func listenLocal(sockPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		return nil, fmt.Errorf("interactiveserve: prepare socket directory: %w", err)
	}
	if info, err := os.Lstat(sockPath); err == nil && info.Mode()&os.ModeSocket != 0 {
		if conn, dialErr := net.Dial("unix", sockPath); dialErr == nil {
			_ = conn.Close()
			return nil, fmt.Errorf("interactiveserve: runtime already has a live interactive-serve session (socket %s is dialable)", sockPath)
		}
		if err := os.Remove(sockPath); err != nil {
			return nil, fmt.Errorf("interactiveserve: remove stale socket: %w", err)
		}
	}
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(sockPath, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}
