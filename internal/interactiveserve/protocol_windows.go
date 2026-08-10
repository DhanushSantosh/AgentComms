//go:build windows

package interactiveserve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// dialLocal dials a named pipe at socketPath (a `\\.\pipe\...` address).
// Bounded by dialTimeout, the same as protocol_unix.go's dialLocal — it
// only needs to prove the pipe is live, not wait out an actual delivery.
func dialLocal(ctx context.Context, socketPath string) (net.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	return winio.DialPipeContext(dialCtx, socketPath)
}

// interactivePipeConfig matches the exact owner-only SDDL descriptor
// internal/daemon/listener_windows.go already established for this
// project's daemon control socket -- kept local here rather than
// cross-imported since these are otherwise unrelated subsystems (matching
// protocol_unix.go's listenLocal, and RFC 0010's stated philosophy for
// this package).
var interactivePipeConfig = &winio.PipeConfig{
	SecurityDescriptor: "D:P(A;;GA;;;OW)",
	InputBufferSize:    64 * 1024,
	OutputBufferSize:   64 * 1024,
}

// listenLocal binds sockPath as a named pipe.
//
// Unlike a unix domain socket, CreateNamedPipe allows UNLIMITED concurrent
// server instances for the same pipe name by default -- confirmed by
// reading go-winio's own source (pipe.go's makeServerPipeHandle hardcodes
// nMaxInstances to 0xffffffff), not assumed. Calling winio.ListenPipe alone
// would therefore let two interactive-serve processes both silently "own"
// the same runtime's control pipe -- exactly the double-attachment
// collision Takeover's doc comment describes as a real, confirmed problem
// on unix. A genuine, atomic mutual-exclusion primitive is required
// alongside the pipe: a probe-then-act dial check would leave a real
// TOCTOU race that unix's own listenLocal doesn't have (its stale-vs-live
// check is backed by the filesystem's own atomicity, which a bare named
// pipe doesn't give here). This reuses the exact LockFileEx(...,
// LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY) idiom
// internal/projectlifecycle/lock_windows.go already established for the
// same class of problem, kept local rather than cross-imported per this
// package's own convention.
func listenLocal(sockPath string) (net.Listener, error) {
	lockPath := lockMarkerPath(sockPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, fmt.Errorf("interactiveserve: prepare lock directory: %w", err)
	}
	lockFile, err := acquireExclusiveLock(lockPath)
	if err != nil {
		return nil, fmt.Errorf("interactiveserve: runtime already has a live interactive-serve session (lock %s is held): %w", lockPath, err)
	}
	listener, err := winio.ListenPipe(sockPath, interactivePipeConfig)
	if err != nil {
		_ = releaseExclusiveLock(lockFile)
		return nil, err
	}
	return &lockedListener{Listener: listener, lockFile: lockFile}, nil
}

// lockMarkerPath derives a short, deterministic lock-marker file path from
// a pipe address, stored under socketRootDir() -- repurposed on Windows to
// hold these markers rather than socket files, since a named pipe address
// isn't a filesystem path at all (see socket_root_windows.go).
func lockMarkerPath(sockPath string) string {
	hash := sha256.Sum256([]byte(sockPath))
	return filepath.Join(socketRootDir(), hex.EncodeToString(hash[:8])+".lock")
}

// lockedListener wraps a named-pipe net.Listener so its Close() also
// releases the mutual-exclusion lock listenLocal acquired -- Serve's
// `defer listener.Close()` (shared call-site pattern with the unix
// implementation) then correctly releases both without needing any
// Windows-specific cleanup code in serve_windows.go itself.
type lockedListener struct {
	net.Listener
	lockFile *os.File
}

func (l *lockedListener) Close() error {
	closeErr := l.Listener.Close()
	lockErr := releaseExclusiveLock(l.lockFile)
	if closeErr != nil {
		return closeErr
	}
	return lockErr
}

// acquireExclusiveLock takes a non-blocking exclusive lock on path,
// creating it if necessary -- the same LockFileEx(...,
// LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY) idiom
// internal/projectlifecycle/lock_windows.go uses for the equivalent
// project-lifecycle problem. The marker file is intentionally never
// deleted (matching that package's own convention) -- only ever
// unlocked+closed, so the next acquire simply reopens and relocks it.
func acquireExclusiveLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var overlapped windows.Overlapped
	err = windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &overlapped)
	if err != nil {
		_ = file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, errors.New("another interactive-serve process already holds this runtime's lock")
		}
		return nil, err
	}
	return file, nil
}

func releaseExclusiveLock(file *os.File) error {
	if file == nil {
		return nil
	}
	var overlapped windows.Overlapped
	err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
