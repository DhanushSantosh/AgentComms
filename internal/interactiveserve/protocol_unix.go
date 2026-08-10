//go:build !windows

package interactiveserve

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// dialLocal dials a unix domain socket at socketPath.
func dialLocal(ctx context.Context, socketPath string) (net.Conn, error) {
	dialer := net.Dialer{Timeout: dialTimeout}
	return dialer.DialContext(ctx, "unix", socketPath)
}

// listenLocal binds sockPath, refusing to start if another live process
// already owns it and cleaning up a stale leftover from a crashed one.
// Mirrors the exact sequence internal/daemon/listener_unix.go's ListenLocal
// already proves out for this project's daemon control socket, kept local
// here rather than cross-imported since these are otherwise unrelated
// subsystems.
func listenLocal(sockPath string) (net.Listener, error) {
	socketDirectory := filepath.Dir(sockPath)
	if err := os.MkdirAll(socketDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("interactiveserve: prepare socket directory: %w", err)
	}
	if err := os.Chmod(socketDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("interactiveserve: secure socket directory: %w", err)
	}
	if _, err := os.Lstat(sockPath); err == nil {
		var d net.Dialer
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		conn, dialErr := d.DialContext(ctx, "unix", sockPath)
		cancel()
		if dialErr == nil {
			_ = conn.Close()
			return nil, fmt.Errorf("interactiveserve: runtime already has a live interactive-serve session (socket %s is dialable)", sockPath)
		}
		if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("interactiveserve: remove stale socket: %w", err)
		}
	}
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(sockPath, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}
