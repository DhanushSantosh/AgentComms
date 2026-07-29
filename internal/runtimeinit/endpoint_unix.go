//go:build !windows

package runtimeinit

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
)

const sharedUnixRuntimeRoot = "/tmp"

func DaemonEndpoint(_ string, projectID string) string {
	projectHash := sha256.Sum256([]byte(projectID))
	socketName := hex.EncodeToString(projectHash[:8]) + ".sock"
	return filepath.Join(
		sharedUnixRuntimeRoot,
		"agent-comms-"+strconv.Itoa(os.Geteuid()),
		"daemon",
		socketName,
	)
}
