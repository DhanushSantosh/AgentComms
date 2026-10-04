//go:build !unix && !windows

package worker

import (
	"context"
	"errors"
	"os/exec"
)

type ownedExec struct{}

func prepareOwnedExec(*exec.Cmd) (*ownedExec, error) {
	return nil, errors.New("owned provider execution is unsupported on this platform")
}
func (*ownedExec) started(context.Context, *exec.Cmd) error { return nil }
func (*ownedExec) terminate() error                         { return nil }
func (*ownedExec) release()                                 {}
