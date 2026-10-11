//go:build windows

// Package pipeacl builds the security descriptor for this project's local
// named pipes (the per-project daemon and interactive-serve control pipes).
package pipeacl

import (
	"fmt"
	"sync"

	"golang.org/x/sys/windows"
)

var (
	once       sync.Once
	descriptor string
	resolveErr error
)

// CurrentUserOnly returns an SDDL descriptor that grants full access to the
// current user's SID and nobody else.
//
// It replaces "D:P(A;;GA;;;OW)" (OWNER RIGHTS). That form looked equivalent
// and is not: an elevated process's token names BUILTIN\Administrators as the
// default owner of objects it creates, not the user. Confirmed live: a daemon
// started from an "Administrator" terminal created a pipe that every
// ordinary (non-elevated) agent-comms call by the same user was then denied
// with "Access is denied" -- and the reverse also holds -- so a project
// worked or failed depending on which kind of terminal happened to start
// its daemon first. The user SID is the same in both tokens, which keeps the
// pipe exactly as private as intended while letting both reach it.
func CurrentUserOnly() (string, error) {
	once.Do(func() {
		user, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			resolveErr = fmt.Errorf("resolve current user SID for pipe security: %w", err)
			return
		}
		descriptor = "D:P(A;;GA;;;" + user.User.Sid.String() + ")"
	})
	return descriptor, resolveErr
}
