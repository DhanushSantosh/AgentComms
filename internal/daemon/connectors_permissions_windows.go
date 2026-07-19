package daemon

import "os"

func connectorConfigPermissionsPrivate(_ os.FileInfo) bool {
	// Windows access control is represented by ACLs rather than POSIX mode bits.
	// os.FileMode therefore cannot determine whether a file is private.
	return true
}
