//go:build !linux && !windows

package interactiveserve

import (
	"os/user"
	"path/filepath"
)

const sharedUnixTemporaryRoot = "/tmp"

func socketRootDir() string {
	userID := "unknown"
	if currentUser, err := user.Current(); err == nil && currentUser.Uid != "" {
		userID = currentUser.Uid
	}
	return filepath.Join(sharedUnixTemporaryRoot, "agent-comms-"+userID, "interactive")
}
