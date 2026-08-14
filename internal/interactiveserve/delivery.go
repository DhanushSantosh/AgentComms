package interactiveserve

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// This file holds the control-socket delivery logic shared by both the unix
// pty implementation (serve.go) and the Windows ConPTY implementation
// (serve_windows.go). Neither the busy/echo heuristics (matcher.go) nor this
// socket-serving logic touch a real file descriptor directly -- they only
// need something that accepts terminal input as bytes, so everything here is
// written against io.Writer rather than *os.File, letting both platforms
// share one hardened implementation of the parts that have nothing to do
// with how a given OS allocates a pty.

// idleTimeout bounds how long deliverToPty waits for a busy target to come
// up for air before giving up rather than injecting into an active turn. It
// is generous because the target may legitimately be deep in its own
// multi-step tool-calling sequence — waiting longer here is what actually
// prevents two deliveries from gluing their text together in the pty's input
// buffer, not a fallback for it.
const idleTimeout = 90 * time.Second

// echoTimeout bounds how long deliverToPty waits, after writing text, for
// the pty to visibly echo it back before writing Enter — never trusting a
// fixed sleep to be long enough for a given provider's input handling.
const echoTimeout = 10 * time.Second

// directDeliveryIdleTimeout keeps invocation creation responsive. A direct
// wake is opportunistic: if the target became busy after the caller's probe,
// it is safer to return a retryable warning than to hold the requester while
// another agent finishes a potentially long turn.
const directDeliveryIdleTimeout = 250 * time.Millisecond

// acceptLoop serves control-socket connections until listener is closed
// (which happens via Serve's deferred cleanup on shutdown).
func acceptLoop(listener net.Listener, tee *outputTee, w writeFlusher, mu *sync.Mutex) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go handleConn(conn, tee, w, mu)
	}
}

// writeFlusher is the minimal interface delivery needs from the underlying
// pty/ConPTY master: write terminal input. Both *os.File (creack/pty's
// ptmx) and *conpty.ConPty satisfy this.
type writeFlusher interface {
	Write(p []byte) (n int, err error)
}

func handleConn(conn net.Conn, tee *outputTee, w writeFlusher, mu *sync.Mutex) {
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}
	switch req.Kind {
	case "ping":
		_ = json.NewEncoder(conn).Encode(Response{OK: true, Busy: isBusy(tee.snapshot())})
	case "snapshot":
		_ = json.NewEncoder(conn).Encode(Response{OK: true, Busy: isBusy(tee.snapshot()), OutputSnapshot: string(tee.snapshot())})
	case "deliver":
		mu.Lock()
		evidence, err := deliverToPtyWithEvidence(w, tee, req.Message, idleTimeout, echoTimeout)
		mu.Unlock()
		resp := Response{OK: err == nil}
		if err == nil {
			resp.TextEchoedAt = &evidence.TextEchoedAt
			resp.EnterSentAt = &evidence.EnterSentAt
		}
		if err != nil {
			resp.Error = err.Error()
		}
		_ = json.NewEncoder(conn).Encode(resp)
	case "try-deliver":
		if !mu.TryLock() {
			_ = json.NewEncoder(conn).Encode(Response{OK: false, Busy: true, Error: "another delivery is already in progress"})
			return
		}
		evidence, err := deliverToPtyWithEvidence(w, tee, req.Message, directDeliveryIdleTimeout, echoTimeout)
		mu.Unlock()
		resp := Response{OK: err == nil, Busy: err != nil && isBusy(tee.snapshot())}
		if err == nil {
			resp.TextEchoedAt = &evidence.TextEchoedAt
			resp.EnterSentAt = &evidence.EnterSentAt
		}
		if err != nil {
			resp.Error = err.Error()
		}
		_ = json.NewEncoder(conn).Encode(resp)
	default:
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: "unknown request kind"})
	}
}

// deliverToPty writes message into w as terminal input: waits for the
// target to be idle (see isBusy) before sending anything — the direct
// replacement for the old cross-process delivery lock, since there's now
// only ever one process touching this pty, concurrent senders just make
// concurrent socket connections and this mutex-guarded function serializes
// them — then writes the text and a separate Enter only once tee has
// visibly reflected the text back, never blind. idleTO/echoTO are the real
// package constants in production; tests call this directly with short
// overrides rather than waiting out the real, deliberately generous values.
func deliverToPty(w writeFlusher, tee *outputTee, message string, idleTO, echoTO time.Duration) error {
	_, err := deliverToPtyWithEvidence(w, tee, message, idleTO, echoTO)
	return err
}

func deliverToPtyWithEvidence(w writeFlusher, tee *outputTee, message string, idleTO, echoTO time.Duration) (DeliveryReceipt, error) {
	if !waitForIdle(tee, idleTO) {
		return DeliveryReceipt{}, fmt.Errorf("target was still busy after %s; not injecting into an in-progress turn", idleTO)
	}
	if _, err := w.Write([]byte(message)); err != nil {
		return DeliveryReceipt{}, fmt.Errorf("write text: %w", err)
	}
	if !waitForEchoBuf(tee, message, echoTO) {
		return DeliveryReceipt{}, errors.New("target never echoed the sent text back within the timeout; refusing to send Enter blind")
	}
	textEchoedAt := time.Now().UTC()
	if _, err := w.Write([]byte("\r")); err != nil {
		return DeliveryReceipt{}, fmt.Errorf("write enter: %w", err)
	}
	enterSentAt := time.Now().UTC()
	return DeliveryReceipt{TextEchoedAt: textEchoedAt, EnterSentAt: enterSentAt}, nil
}

func waitForIdle(tee *outputTee, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !isBusy(tee.snapshot()) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func waitForEchoBuf(tee *outputTee, text string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if echoed(tee.snapshot(), text) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}
