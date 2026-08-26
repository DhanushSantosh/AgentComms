//go:build js

package codexserve

import "syscall"

// js/wasm has no processes to detach from: syscall.SysProcAttr is an empty
// struct there, and os/exec cannot start a child at all inside a browser
// sandbox. nil is the honest answer -- there are no detach attributes to set
// -- and it keeps this package compiling for the WASM demo build, which
// reaches it only through the connector/runtime type graph, never to spawn
// anything. See detach_unix.go and detach_windows.go for the real ones.
func detachedProcAttr() *syscall.SysProcAttr { return nil }
