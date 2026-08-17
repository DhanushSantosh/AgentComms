// js/wasm reports as neither windows nor a real unix: its syscall.SysProcAttr
// has no Setsid field (there are no processes to detach from in a browser
// sandbox), so it must be excluded explicitly rather than swept up by
// "!windows".
//go:build !windows && !js

package opencodeclient

import "syscall"

// detachedProcAttr configures a spawned process to survive independently of
// its parent — required here because the opencode serve instance this
// package manages must outlive any single invocation that happens to be the
// one that started it; it's a shared, long-lived server, not a per-call
// subprocess like the ACP adapters use.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
