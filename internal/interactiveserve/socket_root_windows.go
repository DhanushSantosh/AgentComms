//go:build windows

package interactiveserve

import (
	"os"
	"path/filepath"
)

func socketRootDir() string {
	return filepath.Join(os.TempDir(), "agent-comms-interactive")
}
