// Package pipeacl builds the security descriptor for this project's local
// named pipes (the per-project daemon and interactive-serve control pipes).
// It is Windows-only; on other platforms the package is empty, and those
// endpoints are unix domain sockets protected by file permissions instead.
package pipeacl
