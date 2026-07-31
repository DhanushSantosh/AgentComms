//go:build windows

package projectlifecycle

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func lockFile(path string, timeout time.Duration) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	var overlapped windows.Overlapped
	for {
		err = windows.LockFileEx(windows.Handle(file.Fd()),
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0, 1, 0, &overlapped)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			_ = file.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil, errLifecycleLocked
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func unlockFile(file *os.File) error {
	var overlapped windows.Overlapped
	err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
