# RFC 0014: ConPTY-backed `interactive-serve` on Windows

## Status

**Implemented on `dev`, 2026-08-10.** `internal/interactiveserve/
serve_windows.go` and `takeover_windows.go` now have real implementations;
the design below was largely built as proposed, with three real corrections
live testing forced during implementation — recorded here rather than
silently edited into the original proposal, matching this project's own
convention (see RFC 0010's Status section) for keeping the proposal-vs-
built record honest:

1. **`protocol.go`'s control socket needed a real Windows-specific
   implementation after all — the "maybe `net.Listen(\"unix\", ...)` already
   works unmodified" question this RFC left open resolved to "no."**
   `net.Listen`/`net.Dial("unix", ...)` do work for connectivity on Windows
   10+, confirmed live — but `os.Chmod(sockPath, 0o600)` is a silent no-op
   there, so the owner-only permission guarantee `protocol_unix.go`'s
   `listenLocal` depends on does not carry over. Windows now uses a named
   pipe (`github.com/Microsoft/go-winio`, the same owner-only SDDL
   descriptor `internal/daemon/listener_windows.go` already established) —
   see `protocol_windows.go`. A further finding while building this: unlike
   a unix domain socket, `CreateNamedPipe` allows unlimited concurrent
   server instances for the same pipe name by default (confirmed via
   go-winio's own source), which would have silently permitted two
   `interactive-serve` processes to both attach to one runtime's pipe — the
   exact double-attachment collision `Takeover`'s doc comment describes as
   a real, confirmed problem. Closed with an explicit `LockFileEx(...,
   LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY)` guard reusing
   `internal/projectlifecycle/lock_windows.go`'s exact idiom — live-tested
   (`TestServeSecondInstanceRefusesWhileFirstIsLive`) and confirmed to
   correctly refuse a second instance.
2. **`ConPty.Read` never returns `io.EOF` when the attached process
   exits** — confirmed live, not assumed: the pseudo-console's pipe stays
   open until `Close()` is called explicitly, unlike a real unix pty (EIO
   on slave-close). `Serve` detects child exit via the process handle,
   gives the output-copy goroutine a bounded grace period
   (`outputDrainGrace`, 1s) to finish naturally, then force-closes the
   ConPTY as a fallback — verified live in both timing outcomes (natural
   finish and forced fallback) to still capture full output correctly,
   including ConPTY's own trailing teardown escape sequences.
3. **`GenerateConsoleCtrlEvent` — this RFC's proposed mechanism for both
   `Takeover`'s graceful-stop step and `Serve`'s own signal-forwarding —
   does not reliably work and was dropped, not tuned.** Tested live, twice,
   from a genuinely separate process against an ordinary
   `CREATE_NEW_PROCESS_GROUP` target proven to receive signals via its own
   `signal.Notify(os.Interrupt)` handler (not inferred from a target that
   merely didn't respond): the direct call fails outright
   (`ERROR_INVALID_PARAMETER`), and the documented
   `FreeConsole`+`AttachConsole` workaround also fails outright
   (`AttachConsole` itself returns `ERROR_INVALID_PARAMETER`) — tested from
   both Git Bash and a genuine native PowerShell console, ruling out a
   terminal-emulator confound. Writing a raw Ctrl-C byte into the ConPTY's
   own input channel (bypassing `GenerateConsoleCtrlEvent` entirely) was
   also tested and also did not deliver an interrupt. **Both `Takeover`
   and `Serve`'s own child-shutdown path use `TerminateProcess` directly
   instead — no graceful-signal step.** This is a real, deliberate
   reduction from the unix implementation's SIGTERM-then-SIGKILL courtesy,
   not an oversight; see `takeover_windows.go`'s and `serve_windows.go`'s
   doc comments for the full reasoning. `TerminateProcess` itself was
   reliable in every test performed.

Per `docs/rfcs/README.md` and `docs/development-workflow.md`'s design-
proposal rule, this RFC was reviewed and accepted before implementation
began; the rest of this document is retained as the original proposal and
verification record, updated only where the corrections above apply.

## Context

`interactive-serve` (RFC 0010, hardened by RFC 0013) is host-local,
PTY-owning delivery: `agent-comms runtime interactive-serve --id
<runtimeID> -- <command>` allocates a real pty via `github.com/creack/pty`,
execs the wrapped provider CLI attached to it, forwards the wrapper's own
stdin/stdout so the terminal emulator shows the child's native UI
unmediated, and listens on a control socket so other processes can wake it
with `Deliver` — writing text into the pty, waiting for it to echo back,
then sending Enter, with that echo/Enter pair recorded as durable delivery
evidence (`PTY_TEXT_ECHOED`, `PTY_ENTER_SENT` in RFC 0013's delivery state
machine).

`creack/pty` has no Windows implementation. `internal/interactiveserve/
serve_windows.go` and `takeover_windows.go` currently return a hard error
rather than attempting something unreliable — a deliberate design choice
that predates this RFC and remains correct given no alternative existed
wired in. This is not a regression versus the tmux-backed design
`interactive-serve` replaced: tmux itself never worked on Windows either
(see RFC 0010's Status section).

Confirmed live on Windows 11, 2026-08-09/10: everything else in this
project's Windows story works, including headless WORKER-kind delivery
(`claude-live`/`codex-live`/`opencode-live`, which spawn provider CLIs via
`claudeserve`/`codexserve`/`opencodeclient` rather than a PTY) and the TUI
itself (Bubble Tea v2, rendering correctly under real Windows terminal
emulation). The gap is narrowly the PTY-owning mechanism, not the whole
platform.

## Decision

Replace the Windows stub with a real implementation built on
`github.com/charmbracelet/x/conpty` rather than a general-purpose or
unmaintained ConPTY wrapper. Reasoning: this project already depends on
the charmbracelet ecosystem for exactly this class of terminal work —
`charmbracelet/x/term` is already imported by `serve.go` itself for
`MakeRaw`/`Restore`/`GetSize`, and `charm.land/bubbletea/v2`,
`charmbracelet/x/ansi`, `charmbracelet/x/termios`, and
`charmbracelet/x/windows` are already direct or indirect module
dependencies (`go.mod`). Its `ConPty` type exposes `Read`/`Write` like
`creack/pty`'s `ptmx` `*os.File`, plus `Spawn`, `Resize`, `Size`, and
`Close` — the same shape `serve.go` already codes against, so the unix and
Windows implementations can share the delivery/echo/idle-detection logic
in `matcher.go` and the socket protocol in `protocol.go` unchanged, with
only the pty-allocation and process-lifecycle layer forking per platform
(exactly how `serve.go`/`serve_windows.go` are already split).

Caveats to carry into implementation, not resolved by this RFC: the
package is pre-v1 (`v0.2.0`, published 2025-11-17) and its own docs don't
expose a `Wait`/exit-code call — the implementation must track the
process via the handle `Spawn` returns and Windows process-wait APIs
(`golang.org/x/sys/windows`, already a dependency) directly, the same
pattern `internal/projectlifecycle/lock_windows.go` already uses for
`LockFileEx`.

### What changes, mapped to the unix implementation it must match

| Unix mechanism (`serve.go`, `takeover.go`, `protocol.go`) | Windows problem | Proposed Windows mechanism |
|---|---|---|
| `pty.StartWithSize` (`creack/pty`) | No Windows implementation | `conpty.New` + `Spawn` (`charmbracelet/x/conpty`) |
| `term.MakeRaw`/`Restore`/`GetSize` (`charmbracelet/x/term`) | None — already cross-platform | No change; this project's own TUI already proves this works on Windows |
| `SIGWINCH` on terminal resize | No POSIX signals on Windows at all | Poll `term.GetSize` on an interval (proposed: 250ms) and call `ConPty.Resize` on change. See "Unresolved questions" — event-based resize via `ReadConsoleInputW`'s `WINDOW_BUFFER_SIZE_EVENT` is a documented alternative, not adopted here as the default because it requires taking over the console's input-event loop, a bigger surface than a resize poll. |
| `syscall.Kill(pid, SIGTERM)` → `SIGKILL` escalation (`takeover.go`, `forwardAndWait` in `serve.go`) | No POSIX signals | **Corrected during implementation, see Status:** `windows.GenerateConsoleCtrlEvent` — both directly and via the documented `AttachConsole` workaround — does not reliably deliver to an unrelated or ConPTY-attached process; confirmed live, not assumed. Shipped as `windows.TerminateProcess` directly, no graceful-signal step, spawned with `CREATE_NEW_PROCESS_GROUP` regardless (matching this project's `detachedProcAttr()` convention elsewhere, even though the signal escalation it would have enabled isn't used). |
| `ps -o ppid= -p <pid>` process-ancestry walk (`currentProcessIsDescendantOf` in `takeover.go`) | No `ps` on Windows | `CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)` + `Process32First`/`Process32Next`, walking `th32ParentProcessID` the same bounded-depth way `currentProcessIsDescendantOf` already does — a small, self-contained syscall wrapper, no new dependency. Live-tested in both directions (`TestCurrentProcessIsDescendantOfOwnParent`, `TestCurrentProcessIsNotDescendantOfItsOwnChild`). |
| `net.Listen("unix", sockPath)` / `net.Dial("unix", ...)` (`protocol.go`'s `listenLocal`/`call`) | **Corrected during implementation, see Status:** connectivity works, but `os.Chmod` is a silent no-op | `github.com/Microsoft/go-winio` named pipes, reusing `internal/daemon/listener_windows.go`'s exact owner-only SDDL descriptor, plus a `LockFileEx`-guarded mutual-exclusion marker (`protocol_windows.go`) since `winio.ListenPipe` alone permits unlimited concurrent instances of the same pipe name. |
| `socket_root_windows.go`'s `socketRootDir()` (`os.TempDir()+"agent-comms-interactive"`) | Already existed, was dead code since `Serve` never ran | Repurposed to hold `protocol_windows.go`'s lock-marker files rather than socket files — a named pipe address isn't a filesystem path, so `SocketPath`'s Windows branch (`socket_address_windows.go`) doesn't use this directory at all; only the lock guard does. |
| `isBusy`/`echoed`/`outputTee` (`matcher.go`) | None — pure string/byte logic, no build tag | No change. The busy-marker heuristic (`"esc to interrupt"` etc.) and ANSI-stripping are provider-UI-dependent, not OS-dependent, and already apply identically once the byte stream is flowing from a ConPTY instead of a unix pty. |
| `AGENT_COMMS_ACTOR`/Claude-session-marker env stripping (`childEnviron` in `serve.go`) | None — pure string logic | No change; reuse as-is for the Windows child's environment. |

### Scope boundary

This RFC covers `Serve()` and `Takeover()` only — bringing
`interactive-serve`/`--takeover-pid` to feature parity on Windows. It does
**not** cover:

- Any change to the daemon-side delivery/evidence model (RFC 0013) —
  `PTY_TEXT_ECHOED`/`PTY_ENTER_SENT` evidence already generalizes to a
  ConPTY-backed delivery unchanged.
- `terminallaunch`'s Windows path (`wt.exe`/`cmd start`) — already works,
  unrelated to PTY ownership.
- Session ID auto-discovery/pinning (`resume.go`, `sessionbind`) — already
  cross-platform.
- Any new provider adapter — this is transport, not a new `claude`/
  `codex`/`opencode` integration.

## Alternatives considered

- **`marcomorain/go-conpty`**: rejected. Public research surfaced known
  intermittent failures and `ERROR_INSUFFICIENT_BUFFER` issues from
  `CreatePseudoConsole` — a worse starting point than a library from an
  ecosystem this project already trusts and already depends on for
  adjacent terminal work.
- **`UserExistsError/conpty` or `qsocket/conpty-go`**: viable
  alternatives, not chosen. Both are reasonable, maintained wrappers, but
  neither has any existing relationship to this project's dependency
  tree — adopting `charmbracelet/x/conpty` instead means one fewer new
  vendor to trust and audit, and keeps `serve.go`/`serve_windows.go`'s
  terminal-handling dependencies (`term`, `ansi`, `conpty`) under one
  organization. Worth revisiting only if `charmbracelet/x/conpty`'s pre-v1
  API instability becomes a real, specific blocker during implementation
  — see "Unresolved questions."
- **`microsoft/hcsshim`'s internal `conpty` package**: rejected outright —
  it's an unexported internal package of a Hyper-V container-hosting
  project, not a published, importable API, and pulling in `hcsshim` as a
  dependency for one internal type would be a large, unrelated surface
  for this project to take on.
- **Event-based resize (`ReadConsoleInputW` + `WINDOW_BUFFER_SIZE_EVENT`)
  instead of polling**: considered, not adopted as the default. It's the
  more "correct" mechanism in the sense of being push- rather than
  poll-based, but it requires owning the console's input-event read loop
  in a way that could conflict with `ConPty`'s own I/O handling and adds
  meaningfully more Windows Console API surface for a resize signal that
  a 250ms poll already answers acceptably (SIGWINCH-driven resize on the
  unix side isn't instant either — it's bounded by however fast the
  terminal emulator delivers the signal). Left as a documented option if
  polling proves visibly laggy in practice, not ruled out permanently.
- **Do nothing; leave Windows on the documented-limitation path shipped
  alongside issue #17's first fix (`doctor` warning + docs callout)**:
  this is the status quo this RFC proposes moving past, not a competing
  design — recorded here only because it's the real alternative to
  building anything at all, and remains an acceptable outcome if this RFC
  is not accepted in review.

## Compatibility and rollout

- **New dependency**: `github.com/charmbracelet/x/conpty` (MIT). Direct,
  Windows-only (`//go:build windows`), so it never affects
  `go build`/`go vet`/`go test` on Linux or macOS — matches this
  project's existing pattern for `github.com/Microsoft/go-winio`, already
  a direct but Windows-relevant dependency.
- **Minimum Windows version**: ConPTY itself requires Windows 10 1809
  (October 2018 Update) or later — earlier Windows 10 builds and all
  prior Windows versions have no ConPTY API at all. This is a real,
  visible new platform floor for this one feature specifically (every
  other Windows code path in this project has no such floor). It must be
  stated explicitly in `docs/site/agents/interactive.md` and checked at
  runtime: `Serve` should detect an unsupported Windows build and fail
  with a clear, actionable error naming the required version, rather than
  a raw ConPTY API failure.
- **No schema, protocol, or CLI-contract change.** `runtime
  interactive-serve` and `--takeover-pid` gain a working Windows
  implementation; their flags, socket protocol (`protocol.go`'s
  `Request`/`Response`), and delivery-evidence shape are unchanged. No
  RFC 0013 event/schema version bump is implied.
- **Rollout**: land behind normal CI (this makes `windows-latest` actually
  exercise `Serve`/`Takeover` for the first time — today's CI runs
  `go test ./...` on Windows, but the current stub means the real PTY
  logic in `serve.go` has never once executed on a Windows CI runner).
  No feature flag proposed — a Windows build simply gains a working
  command it previously didn't have; there's no existing Windows
  behavior to regress.
- **`doctor`'s `INTERACTIVE_RUNTIME_UNSUPPORTED_ON_WINDOWS` finding**
  (shipped ahead of this RFC, closing #17's documentation half) must be
  removed or narrowed to the pre-1809 case once this lands, so it stops
  firing for a platform that now genuinely works.

## Security and privacy implications

- The owner-only socket permissioning this project already enforces on
  unix (`0o600` file mode, `0o700` directory, `listenLocal`'s
  stale-socket/live-socket collision check) needs a Windows-equivalent
  guarantee regardless of which transport (`AF_UNIX` vs. named pipe) is
  used. If `net.Listen("unix", ...)` is confirmed to work on Windows, its
  ACL/permission model needs explicit verification — Windows `AF_UNIX`
  socket files may not honor the same access-restriction guarantees a
  unix filesystem mode bit does. If the go-winio named-pipe fallback is
  used instead, reuse the exact SDDL descriptor
  `internal/daemon/listener_windows.go` already uses
  (`"D:P(A;;GA;;;OW)"`, owner-only), which is already a reviewed,
  shipped pattern in this codebase.
- `CTRL_BREAK_EVENT` via `GenerateConsoleCtrlEvent` is delivered to an
  entire process group, not a single process — confirm the ConPTY-spawned
  child's process group contains only the intended child (and its own
  descendants), not siblings, before relying on this for graceful
  shutdown, so `Takeover` can't accidentally signal an unrelated process.
- No change to what crosses the wire: delivered text is still always this
  package's own fixed notification template (`NotifyInvocation`), never
  raw instruction content — the integrity boundary RFC 0013 already
  documents is unaffected by which OS owns the pty.

## Test and rollout plan

- Unit tests for the Windows-specific pieces, mirroring `serve_test.go`'s
  existing unix coverage where the underlying primitive allows it: process
  descendant-walk (`Toolhelp32Snapshot`-based), graceful-then-forced
  termination escalation, and resize-on-poll behavior. `matcher.go` and
  `protocol.go`'s existing tests need no changes if those files stay
  build-tag-free as designed.
- `windows-latest` CI (already in `ci.yml`'s matrix) will, for the first
  time, actually exercise `Serve`/`Takeover`'s real logic rather than the
  stub's immediate error return — this alone is meaningfully more
  coverage than exists today.
- A manual smoke test gated behind an env var, matching this project's
  existing per-adapter convention (`internal/worker/smoke_manual_*_test.go`):
  start a real `interactive-serve`-wrapped `codex`/`opencode` session on
  Windows, delivered an invocation from a second process, and confirm
  both the visible terminal and the recorded `PTY_TEXT_ECHOED`/
  `PTY_ENTER_SENT` evidence match what the unix implementation already
  produces for the same scenario.
- Explicit verification step, early in implementation, before writing any
  other Windows-specific code: confirm whether `protocol.go`'s existing
  `net.Listen("unix", ...)`/`net.Dial("unix", ...)` calls actually work
  unmodified on the Windows 10/11 CI image and on a real Windows 11
  desktop. This determines whether the go-winio fallback in the table
  above is needed at all, and should be resolved and recorded before the
  rest of the implementation proceeds, not discovered late.
- `--takeover-pid` needs its own live cross-check of the "calling process
  is a descendant of pid" refusal (the real, confirmed-live Unix bug this
  check exists for — see `takeover.go`'s doc comment) using the
  Toolhelp32Snapshot-based walk, not assumed to carry over correctly
  just because the unix version is tested.

## Unresolved questions

Resolved during implementation (2026-08-10), recorded here for the audit
trail rather than deleted:

- **`charmbracelet/x/conpty`'s pre-v1 API held up fine in practice.** No
  bugs or instability encountered; the missing `Wait`/exit-code call was a
  real but small gap, closed with `golang.org/x/sys/windows`'s
  `WaitForSingleObject`/`GetExitCodeProcess` directly against the handle
  `Spawn` returns, confirmed reliable across every live test performed
  (including the `outputDrainGrace` timing races described in Status).
  Not revisited to `UserExistsError/conpty`/`qsocket/conpty-go`.
- **The `AF_UNIX` control socket did not work unmodified** — see Status
  item 1. Named pipes with a `LockFileEx` mutual-exclusion guard shipped
  instead.
- **250ms resize polling** shipped as designed; no live signal it reads as
  laggy, though this wasn't specifically stress-tested against rapid
  window dragging. Left as the current behavior — the event-based
  alternative remains a documented option if that ever surfaces as a real
  complaint, not pursued speculatively.
- **Minimum supported Windows build** (10 1809+) is stated in
  `docs/site/agents/interactive.md` as a `[!NOTE]` callout and enforced
  implicitly by `conpty.New`'s own error on an unsupported build — no
  separate `doctor` check was added for this (see `internal/doctor/
  doctor.go`'s comment at the interactive-runtime check for why: it's rare
  enough in practice that a clear error at the point of failure was judged
  sufficient, consistent with how this codebase handles other uncommon
  environment gaps).
