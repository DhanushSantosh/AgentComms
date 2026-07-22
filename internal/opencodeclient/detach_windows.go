//go:build windows

package opencodeclient

import "syscall"

// detachedProcAttr configures a spawned process to survive independently of
// its parent. On Windows, CREATE_NEW_PROCESS_GROUP detaches the child from
// the parent's console/signal group, the closest equivalent to Unix
// setsid for this purpose.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
