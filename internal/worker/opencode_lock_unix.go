//go:build !windows && !js && !plan9

package worker

import (
	"errors"
	"os"
	"syscall"
)

func lockOpenCodeFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("OpenCode runtime/session is already owned by another worker"), file.Close())
	}
	return file, nil
}

func unlockOpenCodeFile(file *os.File) error {
	if file == nil {
		return nil
	}
	return errors.Join(syscall.Flock(int(file.Fd()), syscall.LOCK_UN), file.Close())
}
