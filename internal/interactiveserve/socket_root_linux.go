//go:build linux

package interactiveserve

import (
	"os"
	"path/filepath"
	"strconv"
)

const sharedLinuxTemporaryRoot = "/tmp"

func socketRootDir() string {
	return filepath.Join(
		sharedLinuxTemporaryRoot,
		"agent-comms-"+strconv.Itoa(os.Geteuid()),
		"interactive",
	)
}
