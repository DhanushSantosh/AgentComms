package interactiveserve

import (
	"context"
	"encoding/json"
	"fmt"
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
	OK             bool       `json:"ok"`
	Error          string     `json:"error,omitempty"`
	Busy           bool       `json:"busy,omitempty"` // set on "ping" responses
	TextEchoedAt   *time.Time `json:"text_echoed_at,omitempty"`
	EnterSentAt    *time.Time `json:"enter_sent_at,omitempty"`
	OutputSnapshot string     `json:"output_snapshot,omitempty"` // set on "snapshot" responses
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
// wrapper around this. The actual dial is platform-split (dialLocal, in
// protocol_unix.go/protocol_windows.go) since Windows uses a named pipe
// rather than a unix domain socket -- see listenLocal's doc comment in
// protocol_windows.go for why.
func call(ctx context.Context, socketPath string, req Request) (Response, error) {
	conn, err := dialLocal(ctx, socketPath)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	deadline := time.Now().Add(connDeadline)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return Response{}, err
	}
	stopCancellation := context.AfterFunc(ctx, func() {
		_ = conn.SetDeadline(time.Now())
	})
	defer stopCancellation()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, fmt.Errorf("interactiveserve: send request: %w", err)
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("interactiveserve: read response: %w", err)
	}
	return resp, nil
}
