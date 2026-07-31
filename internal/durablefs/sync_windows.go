//go:build windows

package durablefs

import (
	"errors"
	"os"
)

// SyncDirectory is a no-op because Windows does not expose directory handles
// through os.File.Sync. File data is synchronized before the atomic rename.
func SyncDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("directory synchronization path is not a directory")
	}
	return nil
}
