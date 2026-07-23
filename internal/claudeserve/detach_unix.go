//go:build !windows

package claudeserve

import "syscall"

func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
