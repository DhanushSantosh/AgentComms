//go:build !windows

package daemon

import "os"

func connectorConfigPermissionsPrivate(info os.FileInfo) bool {
	return info.Mode().Perm()&0o077 == 0
}
