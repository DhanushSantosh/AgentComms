package acpclient

import (
	"io"

	acpsdk "github.com/coder/acp-go-sdk"
)

// newPipeSession wires a Session directly to a peer input/output pair,
// bypassing process spawning, so the protocol plumbing can be exercised
// against an in-process fake agent in tests.
func newPipeSession(config Config, peerInput io.Writer, peerOutput io.Reader) *Session {
	session := &Session{config: config}
	session.conn = acpsdk.NewClientSideConnection(session, peerInput, peerOutput)
	return session
}
