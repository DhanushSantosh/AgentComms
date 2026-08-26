// js/wasm reports as neither windows nor a real unix: its syscall.SysProcAttr
// has no Setsid field (there are no processes to detach from in a browser
// sandbox), so it must be excluded explicitly rather than swept up by
// "!windows".
//go:build !windows && !js

package claudeserve

import "syscall"

func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
