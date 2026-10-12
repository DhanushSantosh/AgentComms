package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/store"
)

// interactiveHeartbeatRetry is how soon a failed interactive heartbeat is
// retried. It is deliberately not shorter than
// controlplane.MinHeartbeatInterval's spirit: a heartbeat that timed out on
// the client may still have committed on the authority, and a retry that
// lands too soon is rejected as "interval must be at least" -- which
// interactiveHeartbeatLoop treats as proof a beat landed, not as a failure.
const interactiveHeartbeatRetry = 5 * time.Second

// interactiveHeartbeatLoop keeps an interactive runtime's presence fresh for
// as long as ctx lives, and never ends the session itself.
//
// It used to cancel the whole interactive-serve context on the first
// heartbeat error, which terminated the wrapped agent mid-turn. Confirmed
// live: two real Codex/Claude sessions on one project were both killed at
// the same second with "interactive runtime heartbeat: context deadline
// exceeded" while their daemon was being swapped between two builds. A
// heartbeat is presence bookkeeping; losing a few of them only means the
// authority marks the runtime OFFLINE after controlplane.RuntimeOfflineAfter,
// and the dispatcher stops routing deliveries to it until the next beat
// lands. The agent's own work must never depend on it. The same applies to
// every other rejection (a runtime drained or revoked by an operator while
// the session is live): the dispatcher already refuses to deliver to such a
// runtime, so the loop keeps trying at the normal cadence and reports what
// happened instead of killing anything.
type interactiveHeartbeatLoop struct {
	beat     func() error
	interval time.Duration
	retry    time.Duration
	report   func(string)

	mu          sync.Mutex
	failures    int
	consecutive int
	lastErr     error
	lastErrAt   time.Time
}

func (h *interactiveHeartbeatLoop) run(ctx context.Context) {
	timer := time.NewTimer(h.interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		next := h.interval
		if err := h.beat(); err != nil && !heartbeatLandedRecently(err) {
			h.mu.Lock()
			h.failures++
			h.consecutive++
			h.lastErr, h.lastErrAt = err, time.Now()
			consecutive := h.consecutive
			h.mu.Unlock()
			if h.report != nil {
				h.report(fmt.Sprintf("heartbeat failed (%d in a row): %v", consecutive, err))
			}
			if consecutive <= 3 {
				next = h.retry
			}
		} else {
			h.mu.Lock()
			recovered := h.consecutive > 0
			h.consecutive = 0
			h.mu.Unlock()
			if recovered && h.report != nil {
				h.report("heartbeat recovered")
			}
		}
		timer.Reset(next)
	}
}

// heartbeatLandedRecently reports whether err is the authority refusing a
// beat only because another one committed moments ago -- typically the
// previous attempt, which timed out on the client after the authority had
// already accepted it.
func heartbeatLandedRecently(err error) bool {
	return err != nil && strings.Contains(err.Error(), "heartbeat interval must be at least")
}

// summary describes heartbeat trouble for the operator once the session has
// ended and the terminal is back in normal mode. Empty when there was none.
func (h *interactiveHeartbeatLoop) summary() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failures == 0 {
		return ""
	}
	return fmt.Sprintf("%d interactive runtime heartbeat(s) failed during this session (last at %s: %v); the session itself was not interrupted",
		h.failures, h.lastErrAt.Format(time.RFC3339), h.lastErr)
}

// interactiveSessionLog appends timestamped diagnostics for one interactive
// runtime to a file under the project's runtime tmp directory. It exists
// because interactive-serve cannot print anything while it runs: the wrapped
// agent owns the terminal, and a stray line would corrupt its UI.
type interactiveSessionLog struct {
	mu   sync.Mutex
	path string
}

func newInteractiveSessionLog(projectRoot, runtimeID string) *interactiveSessionLog {
	return &interactiveSessionLog{path: filepath.Join(projectRoot, store.Runtime, "tmp",
		"interactive-"+safeLogComponent(runtimeID)+".log")}
}

func (l *interactiveSessionLog) Printf(format string, args ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "%s %s\n", time.Now().UTC().Format(time.RFC3339Nano), fmt.Sprintf(format, args...))
}

// safeLogComponent keeps a runtime ID readable in a file name when it is
// already safe, and hashes it otherwise.
func safeLogComponent(id string) string {
	if id != "" && len(id) <= 48 && id != "." && id != ".." {
		safe := true
		for _, r := range id {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
				safe = false
				break
			}
		}
		if safe {
			return id
		}
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}
