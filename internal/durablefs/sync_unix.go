//go:build !windows

package durablefs

import (
	"errors"
	"os"
)

// SyncDirectory persists directory-entry changes after an atomic rename.
func SyncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	return errors.Join(syncErr, closeErr)
}
