//go:build unix

package worker

import (
	"context"
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

type ownedExec struct{ pid int }

func prepareOwnedExec(command *exec.Cmd) (*ownedExec, error) {
	attr := syscall.SysProcAttr{}
	if command.SysProcAttr != nil {
		attr = *command.SysProcAttr
	}
	attr.Setpgid, attr.Pgid = true, 0
	command.SysProcAttr = &attr
	return &ownedExec{}, nil
}

func (p *ownedExec) started(_ context.Context, command *exec.Cmd) error {
	p.pid = command.Process.Pid
	return nil
}
func (p *ownedExec) release() {}
func (p *ownedExec) terminate() error {
	err := unix.Kill(-p.pid, unix.SIGKILL)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}
