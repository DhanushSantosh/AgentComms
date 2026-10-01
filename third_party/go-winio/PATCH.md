# Local go-winio listener-close correction

`upstream/` is a mechanical copy of `github.com/Microsoft/go-winio v0.6.2`
from the Go module cache (module checksum in the root `go.sum`). Its upstream
MIT license is preserved. Root `go.mod` replaces only this module with the
local copy; supported platforms, pipe ACLs, protocol and dependencies are
unchanged.

## Review map

Only `upstream/pipe.go` differs from that published module. The cancellation
branch of `makeConnectedServerPipe` drains its connect completion, then
unconditionally returns `ErrPipeListenerClosed`. Once that branch consumes
`closeCh`, returning any other error loses the close request. The listener
routine resumes waiting while `Close` waits forever on `doneCh`.

An unexported connector seam and `pipe_close_regression_test.go` make the
completion-error race deterministic with real Windows handles. All other
upstream files are unchanged. No process kill, leaked goroutine workaround,
retry loop, skipped assertion or longer shutdown timeout is used.

Native AgentComms evidence: `deep-startup-baseline.log`, 2026-09-30, Windows
build 28000, Go 1.26.6: 60 rounds of three startup cases, one cleanup failure
(562.123s overall). `daemon.Run` was stuck in `http.Server.Shutdown` calling
`win32PipeListener.Close` at pipe.go:578, while that listener's routine was
already back at its top-level select (pipe.go:462). This distinguishes the
observed lost-close state from a stuck SQLite close or the pending-overlapped
completion conjecture in upstream issue
[357](https://github.com/microsoft/go-winio/issues/357).

Before correction, all three injected Windows completion errors fail the
regression. Run it explicitly (nested-module tests are not included in root
`go test ./...`):

```powershell
go test github.com/Microsoft/go-winio -run '^TestClosePreservesCancellationAcrossConnectErrors$' -count=100
```

This copy is a maintenance obligation, not an upstream release. Replace it
with a published dependency version only after that version preserves this
regression and native shutdown/rebind checks. Record any future upstream
changes here; do not silently refresh the copy.
