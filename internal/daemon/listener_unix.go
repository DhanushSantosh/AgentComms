//go:build !windows

package daemon

import (
	"net"
	"os"
	"path/filepath"
)

func ListenLocal(endpoint string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(endpoint), 0o700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(endpoint); err == nil && info.Mode()&os.ModeSocket != 0 {
		if connection, dialErr := net.Dial("unix", endpoint); dialErr == nil {
			_ = connection.Close()
			return nil, os.ErrExist
		}
		if err = os.Remove(endpoint); err != nil {
			return nil, err
		}
	}
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(endpoint, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}
