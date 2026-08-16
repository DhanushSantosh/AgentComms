//go:build js

package opencodeclient

import "syscall"

// js/wasm has no processes to detach from: syscall.SysProcAttr is an empty
// struct there, and os/exec cannot start a child at all inside a browser
// sandbox. nil is the honest answer -- there are no detach attributes to set
// -- and it keeps this package compiling for the WASM demo build, which
// reaches it only through the connector/runtime type graph, never to spawn
// the long-lived opencode server this file's real counterparts detach.
func detachedProcAttr() *syscall.SysProcAttr { return nil }
