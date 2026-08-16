# Interactive WASM TUI (Control Room) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace section 6 ("Control Room") of `sites/landing`'s homepage, currently a static hand-built recreation (`ControlRoomFrame.tsx`), with a genuinely live, interactive instance of the real product TUI (`internal/tui`) compiled to WebAssembly and driven from the browser via xterm.js, seeded with a real demo project built through the actual `Service.Execute` transition API.

**Architecture:** A new Go WASM entrypoint (`cmd/agent-comms-tui-wasm`) runs the real `internal/tui` + `internal/daemon` + `internal/protocol` code, backed by new pure-Go, SQLite-free, in-memory implementations of the daemon's two storage seams (both already interfaces, or made into one), connected to the daemon over an in-process `net.Pipe()`. A `syscall/js` bridge ships terminal I/O to/from xterm.js in the browser. The existing static `ControlRoomFrame` becomes the pre-launch poster; a "Launch" interaction lazy-loads the WASM module and swaps it for the live terminal in the same frame.

**Tech Stack:** Go 1.26 (`GOOS=js GOARCH=wasm`), `charm.land/bubbletea/v2` (already a dependency, unmodified), `@xterm/xterm` + `@xterm/addon-fit` (new sites/landing dependencies), Next.js 16 static export (`output: "export"`, unchanged).

**Spec:** This document. (Originated from a grill-me interview in the working session; no separate spec file — the "Global Constraints" section below carries every constraint that interview locked in.)

## Global Constraints

- The compiled artifact must be the real `internal/tui` code (`tui.Run`), not a JS/React recreation. Confirmed feasible: `tui.Run(s *service.Service, actor string, in io.Reader, out io.Writer)` already takes injectable I/O, and bubbletea only touches real-terminal raw-mode syscalls when given an actual `*os.File` — neither of which we pass.
- `modernc.org/sqlite` → `modernc.org/libc` has **zero** `js`/`wasm` build files for `errno`, `limits`, `pthread`, `signal`, `stdio`, `sys/types`, `time`, `unistd` — verified by a real `GOOS=js GOARCH=wasm go build ./internal/tui ./internal/daemon` attempt, which fails on exactly those packages. SQLite-backed storage (`internal/localcache.Cache`, `internal/personalauthority.Engine`) cannot compile to WASM as-is. Do not attempt to vendor/patch `modernc.org/libc` — build a parallel in-memory implementation instead (Task 2).
- `internal/daemon.New(cache *localcache.Cache, client authorityClient)` — `client` is already the `authorityClient` interface; `cache` is a concrete `*localcache.Cache` pointer. Task 1 interface-ifies `cache` the same way, with **zero behavior change** for any real caller — `go build ./...` and `go test ./...` must produce byte-identical pass/fail results before and after.
- `m.EnableFileWatch()` must never be called in the WASM entrypoint (fsnotify has no WASM support). No change needed in `internal/tui` for this — `Model.Init()` (internal/tui/model.go) already branches on `m.watcher == nil` and skips the watch command cleanly.
- Seed data goes through the real `Service.Execute(actor, typ, id, payload)` API — never hand-authored JSON/state files. Reference real working call shapes from `internal/tui/tasks_test.go`, `internal/tui/agents_test.go`, `internal/tui/control_room_test.go`, `internal/tui/approvals_test.go`, `internal/service/invocation_test.go`, and the project-bootstrap sequence in `internal/testsupport/project.go`.
- Seed story reuses the exact character/beat set already established in `sites/landing/src/components/ControlRoomFrame.tsx`: agents AXIOM (Release-Coordinator, awaiting approval), DAMON (Frontend-Architect, on `test/auth`), GORGE (Tester, offline); activity sequence `agent.switch-role → task.claim → invocation.request → invocation.claim → approval.request → invocation.start → message.deliver → invocation.complete`. The visitor plays the human owner. Do not invent a different story.
- Curated view coverage only: rich seed data for Overview, Tasks/My work (hubs "Command"/"Work" in `internal/tui/model.go`'s `navigationHubs`), Approvals, Agents ("Team" hub), Activity/History ("Relay" hub). Every other view in `internal/tui/model.go`'s `views` slice stays real and navigable in its genuine empty state — do not seed data for them.
- The seeded approval (AXIOM's HUMAN-tier approval) and the DAMON/GORGE handoff must be left genuinely actionable — a visitor driving them to completion through the live TUI must actually change what renders. Not decorative.
- WASM module + `wasm_exec.js` + xterm.js load lazily, only on a "Launch the Control Room" interaction — never eagerly on page load.
- `ControlRoomFrame` (existing component) is reused unmodified as the pre-launch poster. Do not delete it.
- xterm.js theme must reuse the exact hex values from `internal/tui/model.go`'s `colors(false)` (the non-high-contrast palette): ink `#071216`, panel `#0D2024`, cyan `#56D6C9`, amber `#E8B85C`, coral `#F07167`, lilac `#B9A7E8`, steel `#78918F`, text `#D7E5E3`. These already match `sites/landing/src/app/globals.css`'s CSS custom properties — do not invent new colors.
- Build step must not require new backend infrastructure — output is static assets consistent with `sites/landing/next.config.ts`'s `output: "export"`.
- `sites/landing`'s deploy pipeline (`.github/workflows/deploy-sites.yml`) already runs `actions/setup-go` before `npm ci && npm run build` — the new WASM build step rides that existing Go toolchain, no CI changes needed there.
- Ship as a PR against `dev` (branch protection requires a PR + passing CI; do not push directly). Call out the `internal/daemon.New` signature change as a distinct, easy-to-review real-product-code change, separate from the new WASM/frontend-only code — consider splitting it into its own commit (or even its own PR merged first) rather than bundling it invisibly into a large diff.
- Full verification before considering this done: `go build ./...` + `go test ./...` (repo root, before and after Task 1), `GOOS=js GOARCH=wasm go build ./cmd/agent-comms-tui-wasm` (after Task 3), `npm run check` in `sites/landing`, `npm run lighthouse` (accessibility ≥ 0.96, no new findings), the Docker Playwright suite (`docker run --rm -v "$(pwd):/work" -w /work -e PUBLIC_PRODUCT_VERSION=0.4.0 mcr.microsoft.com/playwright:v1.62.1-noble bash -c "git config --global --add safe.directory /work && cd sites/landing && npm run test:e2e:update"` from repo root), and new Playwright coverage that actually drives the live terminal (Task 6) with screenshot evidence, not just "it compiled."

---

## Task 1: Interface-ify `internal/daemon.New`'s cache parameter

**Files:**
- Modify: `internal/daemon/daemon.go` (the `Daemon` struct's `cache` field, `New`'s signature)
- Modify: `internal/daemon/run.go:65-70` (confirm the real `*localcache.Cache` value it constructs still satisfies the new interface — no call-site change needed since Go satisfies interfaces structurally, but re-read this file to confirm nothing else there assumes a concrete `*localcache.Cache`)
- Test: `internal/daemon/daemon_test.go` (existing tests — must keep passing unchanged; do not weaken any assertion)

**Interfaces:**
- Produces: a new exported-or-unexported interface (name it `cacheStore`, unexported, package `daemon` — matches the existing unexported `authorityClient`/`draftStore` naming convention in `internal/daemon/daemon.go`) with exactly these methods, copied verbatim from `internal/localcache/cache.go`'s real method signatures:
  ```go
  type cacheStore interface {
      VerifyRange(ctx context.Context, projectID string, from, to uint64) error
      Apply(ctx context.Context, event controlplane.Event, receipt controlplane.Receipt) error
      State(ctx context.Context, projectID string) (model.State, controlplane.ResultMetadata, error)
      Events(ctx context.Context, projectID string, page controlplane.PageRequest) (controlplane.EventPage, error)
      SaveDraft(ctx context.Context, draft controlplane.Draft) error
      Drafts(ctx context.Context, projectID string, limit int) ([]controlplane.Draft, error)
  }
  ```
  Do not include `Close() error` — confirmed by reading `internal/daemon/run.go:69` that `Close` is called on the concrete `*localcache.Cache` value directly (`defer cache.Close()`), never through `d.cache`.

- [ ] **Step 1: Record the baseline**

Run from repo root:
```bash
go build ./... 2>&1 | tee /tmp/daemon-iface-before-build.log
go test ./... 2>&1 | tee /tmp/daemon-iface-before-test.log
```
Expected: both succeed (or, if there are pre-existing failures unrelated to this change, record exactly which ones so Step 4 can diff against them).

- [ ] **Step 2: Define the interface and change the field/signature**

In `internal/daemon/daemon.go`, change:
```go
type Daemon struct {
	cache *localcache.Cache
	...
}
```
to:
```go
type Daemon struct {
	cache cacheStore
	...
}
```
Add the `cacheStore` interface definition (shown above) next to the existing `authorityClient`/`draftStore` interface definitions in the same file. Change:
```go
func New(cache *localcache.Cache, client authorityClient) (*Daemon, error) {
```
to:
```go
func New(cache cacheStore, client authorityClient) (*Daemon, error) {
```
Leave every method body in `daemon.go` untouched — they already only call the six methods above through `d.cache`, so no call sites need to change.

- [ ] **Step 3: Confirm `*localcache.Cache` still satisfies the call site in `run.go`**

Read `internal/daemon/run.go` around the `localcache.Open(...)` call and the subsequent `daemon.New(cache, ...)` call. No code change should be needed here — `*localcache.Cache` already has all six methods with matching signatures (confirmed in Task discovery), so it satisfies `cacheStore` automatically. If the compiler disagrees, the mismatch is the signal to fix (a typo'd signature in the interface), not a signal to change `run.go`.

- [ ] **Step 4: Verify zero behavior change**

```bash
go build ./... 2>&1 | tee /tmp/daemon-iface-after-build.log
go test ./... 2>&1 | tee /tmp/daemon-iface-after-test.log
diff /tmp/daemon-iface-before-build.log /tmp/daemon-iface-after-build.log
diff /tmp/daemon-iface-before-test.log /tmp/daemon-iface-after-test.log
```
Expected: no diff (both build cleanly, same test pass/fail set as baseline). If `go vet ./...` and `staticcheck` are part of this repo's normal CI gate (check `.github/workflows/ci.yml`), run those too and confirm no new findings.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "refactor(daemon): accept an interface for the local cache, not a concrete *localcache.Cache

Mirrors the existing authorityClient/draftStore interface pattern.
*localcache.Cache satisfies the new cacheStore interface unchanged, so
every real caller (internal/daemon/run.go) is unaffected -- this is
purely a seam for an alternate, non-SQLite backing store to be
substituted later, starting with an in-memory one for a WASM build
that cannot link modernc.org/sqlite."
```

---

## Task 2: In-memory `authorityClient` and `cacheStore` implementations for WASM

**Files:**
- Create: `internal/wasmdemo/authority.go` (in-memory `authorityClient` implementation)
- Create: `internal/wasmdemo/cache.go` (in-memory `cacheStore` implementation)
- Create: `internal/wasmdemo/authority_test.go`
- Create: `internal/wasmdemo/cache_test.go`

**Interfaces:**
- Consumes: `controlplane.Command`, `controlplane.Event`, `controlplane.Receipt`, `controlplane.PageRequest`, `controlplane.EventPage`, `model.State`, `controlplane.ResultMetadata`, `controlplane.Draft` (all from existing packages, read `internal/controlplane/contracts.go` and `internal/model/*.go` for exact field shapes before writing — do not guess field names).
- Produces:
  - `wasmdemo.NewMemoryAuthority(signer *controlplane.Signer) *MemoryAuthority` implementing `Command(ctx, controlplane.Command) (controlplane.Event, controlplane.Receipt, error)` and `Events(ctx, projectID string, page controlplane.PageRequest) (controlplane.EventPage, error)`.
  - `wasmdemo.NewMemoryCache() *MemoryCache` implementing all six `cacheStore` methods.

This package has no build-tag restriction (it's plain Go, no CGO, no wasm-only syscalls) — write and test it normally with `go test ./internal/wasmdemo/...` on the host platform first. It only becomes wasm-relevant when imported by Task 3's entrypoint.

- [ ] **Step 1: Read the real authority/cache implementations to model behavior**

Read `internal/personalauthority/engine.go` in full (already partially read: `CreateProject`, and the schema showing `projects`/`events` tables with `head_sequence`/`head_hash`) and `internal/localcache/cache.go` in full (`Apply`, `State`, `Events`, `VerifyRange`, `Rebuild`, `SaveDraft`, `Drafts`). These define the exact semantics `MemoryAuthority`/`MemoryCache` must reproduce: an authority that accepts a `controlplane.Command`, validates and appends a hash-chained `controlplane.Event` + `controlplane.Receipt`, and a cache that can `Apply` that event/receipt pair and answer `State`/`Events`/`VerifyRange`/draft queries against it. Do not proceed to Step 2 until you can describe, in your own words, what `Command` and `Apply` each do to project state — if that's unclear from reading, read `internal/controlplane/contracts.go` next for the `Command`/`Event`/`Receipt` field definitions.

- [ ] **Step 2: Write `MemoryAuthority`**

`internal/wasmdemo/authority.go` — a struct holding `sync.Mutex`-protected in-memory state per project (a `map[string]*projectState`, each with the event log as a slice, current `model.State`, sequence counter, and hash chain head), a `*controlplane.Signer` for producing real signed receipts (reuse `controlplane.Signer` unmodified — do not reimplement signing). `Command` validates via the same `internal/protocol` package the real authority uses (confirm by reading how `personalauthority.Engine`'s `Command`-equivalent method invokes `internal/protocol` — reuse that exact call, do not duplicate validation logic).

- [ ] **Step 3: Write the failing test for `MemoryAuthority`**

```go
package wasmdemo

import (
	"context"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
)

func TestMemoryAuthorityAppliesACommandAndReturnsASignedReceipt(t *testing.T) {
	signer, err := controlplane.NewSigner() // read controlplane.Signer's real constructor name/signature before writing this line -- do not guess
	if err != nil {
		t.Fatal(err)
	}
	authority := NewMemoryAuthority(signer)
	ctx := context.Background()
	event, receipt, err := authority.Command(ctx, controlplane.Command{
		ProjectID: "demo", Actor: "owner", Type: "agent.register",
		// fill remaining required Command fields per controlplane.Command's
		// real struct definition, read from internal/controlplane/contracts.go
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Sequence != 1 {
		t.Errorf("expected first event to be sequence 1, got %d", event.Sequence)
	}
	if receipt.Signature == "" {
		t.Error("expected a non-empty receipt signature")
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/wasmdemo/... -run TestMemoryAuthorityAppliesACommandAndReturnsASignedReceipt -v`
Expected: FAIL (package/function doesn't exist yet, or a compile error naming exactly what's missing).

- [ ] **Step 5: Implement `MemoryAuthority` fully, then re-run**

Run: `go test ./internal/wasmdemo/... -run TestMemoryAuthorityAppliesACommandAndReturnsASignedReceipt -v`
Expected: PASS.

- [ ] **Step 6: Write `MemoryCache` following the same TDD cycle**

Same pattern as Steps 2-5: write `internal/wasmdemo/cache.go`'s `MemoryCache` (in-memory `map[string]model.State` plus an event slice per project, `Apply` appends and recomputes state, `State`/`Events`/`VerifyRange` read from the in-memory structures, `SaveDraft`/`Drafts` use a plain `map[string][]controlplane.Draft`), with `internal/wasmdemo/cache_test.go` covering: `Apply` then `State` returns the applied event's resulting state; `Events` returns events in a requested page range; `SaveDraft` then `Drafts` returns it back.

- [ ] **Step 7: Run the full package test suite**

Run: `go test ./internal/wasmdemo/... -v`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/wasmdemo/
git commit -m "feat(wasmdemo): in-memory authorityClient + cacheStore implementations

Pure Go, no CGO, no SQLite -- satisfies internal/daemon's
authorityClient and (post-Task-1) cacheStore interfaces entirely with
in-memory maps/slices. Only consumer is the WASM demo entrypoint
(cmd/agent-comms-tui-wasm); real product code and its real daemon are
completely unaffected."
```

---

## Task 3: WASM entrypoint — in-process daemon + seeded demo project

**Files:**
- Create: `cmd/agent-comms-tui-wasm/main.go`
- Create: `cmd/agent-comms-tui-wasm/seed.go`
- Create: `cmd/agent-comms-tui-wasm/seed_test.go` (build-tag-free — the seed logic itself doesn't need `GOOS=js` to unit test, only `main.go`'s `syscall/js` glue does)

**Interfaces:**
- Consumes: `wasmdemo.NewMemoryAuthority`, `wasmdemo.NewMemoryCache` (Task 2), `daemon.New(cache, client) (*Daemon, error)` and `(*Daemon).Handler() http.Handler` (existing, `internal/daemon/daemon.go`), `service.New(root string) *Service` (existing — note this still calls `store.Open(root).Config()` and expects `daemonclient.New(cfg.DaemonEndpoint, ...)` to be dialable; see Step 2 for how the in-process pipe stands in for that), `tui.Run(s *service.Service, actor string, in io.Reader, out io.Writer) error` (existing, `internal/tui/model.go:1779`).
- Produces: `seedDemoProject(s *service.Service) error` — called once at startup before `tui.Run`, drives the real `Service.Execute` calls for the AXIOM/DAMON/GORGE story.

- [ ] **Step 1: Confirm `GOOS=js GOARCH=wasm` compiles for the dependency chain minus the SQLite path**

```bash
GOOS=js GOARCH=wasm go build ./internal/tui ./internal/daemon ./internal/wasmdemo ./internal/service ./internal/protocol
```
Expected: succeeds now that `internal/daemon` no longer requires `internal/localcache`/`modernc.org/sqlite` to be *linked in* through a concrete type (Go still compiles `internal/localcache` itself if anything transitively imports it for a concrete type elsewhere — if this step still fails on `modernc.org/libc`, grep for any other remaining concrete `*localcache.Cache`/`*personalauthority.Engine` reference reachable from this build graph and report back before proceeding; do not silently work around a second occurrence without understanding it).

- [ ] **Step 2: Establish how `service.Service` reaches the in-process daemon**

Read `internal/service/service.go`'s `configure` function and `executeRemote`/`s.remote *daemonclient.Client` fields, and `internal/daemonclient`'s `New(endpoint string, timeout time.Duration) (*Client, error)` — determine whether `daemonclient.Client` is built on `net/http` (dialing a real address string) or accepts an injectable `http.RoundTripper`/`http.Client`. If it's a plain `http.Client` under the hood (likely, given the daemon exposes `Handler() http.Handler`), the cleanest in-process bridge is **not** `net.Pipe()` directly but an `http.Client` whose `Transport` is a custom `http.RoundTripper` that calls `daemon.Handler().ServeHTTP` in-process against an `httptest.ResponseRecorder`-style adapter, avoiding any real network/socket entirely (works identically under `GOOS=js`, since it's pure Go with no networking syscalls). Confirm this against the actual `daemonclient` source before writing `main.go` — if it turns out `daemonclient.New` truly requires a dialable `net.Listener` address, then fall back to `net.Pipe()` plus `http.Serve` on one end and a custom `net.Conn`-backed `http.Transport.DialContext` on the other (both pure in-memory, still `GOOS=js`-safe). Document which of the two this codebase actually needs, in a comment at the top of `cmd/agent-comms-tui-wasm/main.go`, so the choice isn't silently rediscovered later.

- [ ] **Step 3: Write `main.go`'s non-js-specific bootstrap as a plain, host-testable function**

```go
package main

import (
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/daemon"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/wasmdemo"
)

// bootstrapDemoService wires an in-process daemon backed by the in-memory
// authority + cache from internal/wasmdemo, and returns a real
// *service.Service pointed at it -- no SQLite, no real OS socket, safe
// under GOOS=js. Exported (lowercase, package-internal) so seed_test.go
// can exercise it on the host platform without a browser.
func bootstrapDemoService() (*service.Service, error) {
	signer, err := controlplane.NewSigner() // confirm real constructor signature before writing
	if err != nil {
		return nil, err
	}
	authority := wasmdemo.NewMemoryAuthority(signer)
	cache := wasmdemo.NewMemoryCache()
	d, err := daemon.New(cache, authority)
	if err != nil {
		return nil, err
	}
	return wireServiceToDaemon(d) // implements whichever bridge Step 2 determined
}
```
Leave `wireServiceToDaemon` as the one function whose body depends on Step 2's finding — write it against the real `daemonclient`/`service.Service` construction path once confirmed (do not stub it).

- [ ] **Step 4: Write `seed.go`'s `seedDemoProject`, mirroring `ControlRoomFrame.tsx`'s story exactly**

Reference `sites/landing/src/components/ControlRoomFrame.tsx`'s `workforce` and `activity` arrays as the literal story to reproduce through real `Execute` calls, in this order (payload shapes copied from the real test-file examples named in Global Constraints, not invented):
1. Register `owner` (human) — `Register` or `agent.register`, matching `internal/testsupport/project.go`'s bootstrap pattern (project init already happens via whatever `bootstrapDemoService` → `service.New` requires; confirm whether `owner` registration is already implied by daemon/project creation or needs an explicit call by reading `runtimeinit.Initialize` in `internal/testsupport/project.go`'s reference).
2. `agent.register` + `agent.activate` for AXIOM (Role Release-Coordinator equivalent — check `internal/model`'s `Role` constants for the closest real role name; do not invent a role string that doesn't exist in `internal/model`), DAMON (Frontend-Architect equivalent), GORGE (Tester equivalent, left offline — do not send a `runtime.register` for GORGE).
3. `runtime.register` for AXIOM and DAMON (both `online`), matching `internal/tui/runtimes_test.go`'s and `internal/tui/control_room_test.go`'s real call shape.
4. `task.create` for the `test/auth` task, then `task.claim` by DAMON — matching `internal/tui/tasks_test.go`'s real shapes.
5. `agent.switch-role`, matching activity seq 0142's label (find the real transition type string and payload in `internal/protocol/transitions.go`'s `if typ == "agent.switch-role"` branch, already located during planning research).
6. `invocation.request` (owner → AXIOM), `invocation.claim` (AXIOM), `invocation.start` — matching `internal/tui/control_room_test.go`'s and `internal/service/invocation_test.go`'s real shapes, leaving this invocation in a state a visitor can still act on (do not call `invocation.complete` during seeding — leave it for the visitor to do live, since Global Constraints requires a real actionable item).
7. `approval.request` for AXIOM's HUMAN-tier approval (`internal/tui/approvals_test.go`'s real shape) — **do not** call `approval.approve` during seeding; this is the pending approval a visitor resolves live.
8. `message.post`/`message.deliver` equivalent for "AXIOM → GORGE" (check `internal/tui/inbox_test.go`'s real `message.post` shape).

Each step is its own small function (`seedOwner`, `seedAgents`, `seedRuntimes`, `seedTaskHandoff`, `seedActivityPrelude`, `seedPendingInvocation`, `seedPendingApproval`, `seedMessage`), called in order from `seedDemoProject(s *service.Service) error`, returning the first error encountered (fail loudly — a broken seed should never silently produce a half-empty demo).

- [ ] **Step 5: Write `seed_test.go`**

```go
package main

import "testing"

func TestSeedDemoProjectLeavesAPendingApprovalAndInvocation(t *testing.T) {
	svc, err := bootstrapDemoService()
	if err != nil {
		t.Fatal(err)
	}
	if err := seedDemoProject(svc); err != nil {
		t.Fatal(err)
	}
	state, err := svc.State()
	if err != nil {
		t.Fatal(err)
	}
	foundPendingApproval := false
	for _, approval := range state.Approvals { // confirm real field name on model.State before writing this loop
		if approval.Status == "PENDING" { // confirm real status constant
			foundPendingApproval = true
		}
	}
	if !foundPendingApproval {
		t.Error("expected the seeded demo to leave a pending approval for the visitor to resolve")
	}
	if len(state.Agents) < 3 { // AXIOM, DAMON, GORGE
		t.Errorf("expected at least 3 seeded agents, got %d", len(state.Agents))
	}
}
```

- [ ] **Step 6: Run test to verify it fails, then implement, then verify it passes**

Run: `go test ./cmd/agent-comms-tui-wasm/... -run TestSeedDemoProjectLeavesAPendingApprovalAndInvocation -v`
Expected first: FAIL. After implementing Steps 3-4 fully: PASS.

- [ ] **Step 7: Write `main.go`'s `//go:build js && wasm` entrypoint**

A separate file, `cmd/agent-comms-tui-wasm/wasm_main.go`, guarded with `//go:build js && wasm` at the top, containing `func main()`: calls `bootstrapDemoService()`, `seedDemoProject(svc)`, constructs the `in io.Reader`/`out io.Writer` pair from Task 4's bridge, calls `tui.Run(svc, "owner", in, out)` in a goroutine, and blocks forever (`select {}`) since a WASM program's `main` returning ends the whole module. Do not call `m.EnableFileWatch()` anywhere in this file (Global Constraints).

- [ ] **Step 8: Verify the real WASM build**

```bash
GOOS=js GOARCH=wasm go build -o /tmp/agent-comms-tui.wasm ./cmd/agent-comms-tui-wasm
ls -la /tmp/agent-comms-tui.wasm
```
Expected: succeeds, produces a file (note its size — this is the number that justifies the lazy-load decision in Task 5).

- [ ] **Step 9: Commit**

```bash
git add cmd/agent-comms-tui-wasm/
git commit -m "feat(tui-wasm): new WASM entrypoint running the real TUI against an in-memory seeded demo project

Seeds the same AXIOM/DAMON/GORGE story already told statically in
sites/landing's ControlRoomFrame.tsx, through the real Service.Execute
transition API -- not hand-authored fixtures. Leaves one approval and
one invocation genuinely pending so a live visitor can act on them."
```

---

## Task 4: JS/Go terminal I/O bridge

**Files:**
- Modify: `cmd/agent-comms-tui-wasm/wasm_main.go` (add the `syscall/js` exports)
- Create: `sites/landing/public/tui/wasm-bridge.js` (thin JS glue between xterm.js and the exported Go functions)

**Interfaces:**
- Produces (Go, exposed via `js.Global().Set(...)`):
  - `agentCommsTUIWrite(bytes Uint8Array)` — JS calls this on every xterm.js `onData` keystroke; appends the bytes to the Go-side input buffer that `tui.Run`'s `in io.Reader` reads from.
  - `agentCommsTUIResize(cols, rows int)` — JS calls this on load and on every xterm.js `onResize`; encodes a `uv.WindowSizeEvent`-recognizable byte sequence (confirm the exact encoding by reading `charm.land/bubbletea/v2@v2.0.8`'s `ultraviolet` input-parsing source for `WindowSizeEvent` — the plan's earlier research confirmed it's parsed from the input stream but did not pin down the exact wire encoding; this step must nail that down by reading `github.com/charmbracelet/ultraviolet`'s input parser before writing the encoder) and appends it to the same input buffer.
  - Output side: the Go program's `out io.Writer` pushes bytes to a JS callback (`window.agentCommsTUIOnOutput`, set by `wasm-bridge.js` before starting the Go program) via `js.Global().Call("agentCommsTUIOnOutput", ...)`.
- Consumes (JS side, `wasm-bridge.js`): `@xterm/xterm`'s `Terminal` instance and `@xterm/addon-fit`'s `FitAddon` (added as dependencies in Task 5).

- [ ] **Step 1: Pin down the real WindowSizeEvent wire encoding**

```bash
find /home/dhanush/go/pkg/mod/github.com/charmbracelet/ultraviolet* -iname "*.go" | xargs grep -ln "WindowSizeEvent"
```
Read whichever file(s) that finds, specifically the input parser's handling of in-band resize reports (likely a `CSI ... t` sequence per xterm's `report window size in characters` convention, or a Kitty-protocol-specific encoding — confirm which one ultraviolet actually implements before writing the encoder in Step 3; do not guess between the two).

- [ ] **Step 2: Write the Go-side input buffer as a real `io.Reader`**

In `wasm_main.go`:
```go
type jsInputBuffer struct {
	mu     sync.Mutex
	buf    []byte
	notify chan struct{}
}

func newJSInputBuffer() *jsInputBuffer {
	return &jsInputBuffer{notify: make(chan struct{}, 1)}
}

func (b *jsInputBuffer) write(p []byte) {
	b.mu.Lock()
	b.buf = append(b.buf, p...)
	b.mu.Unlock()
	select {
	case b.notify <- struct{}{}:
	default:
	}
}

func (b *jsInputBuffer) Read(p []byte) (int, error) {
	for {
		b.mu.Lock()
		if len(b.buf) > 0 {
			n := copy(p, b.buf)
			b.buf = b.buf[n:]
			b.mu.Unlock()
			return n, nil
		}
		b.mu.Unlock()
		<-b.notify
	}
}
```

- [ ] **Step 3: Write the Go-side output writer**

```go
type jsOutputWriter struct{}

func (jsOutputWriter) Write(p []byte) (int, error) {
	js.Global().Call("agentCommsTUIOnOutput", string(p))
	return len(p), nil
}
```

- [ ] **Step 4: Wire the two `syscall/js` exports in `main()`**

```go
input := newJSInputBuffer()
js.Global().Set("agentCommsTUIWrite", js.FuncOf(func(this js.Value, args []js.Value) any {
	data := make([]byte, args[0].Get("length").Int())
	js.CopyBytesToGo(data, args[0])
	input.write(data)
	return nil
}))
js.Global().Set("agentCommsTUIResize", js.FuncOf(func(this js.Value, args []js.Value) any {
	cols, rows := args[0].Int(), args[1].Int()
	input.write(encodeWindowSizeEvent(cols, rows)) // implements Step 1's real encoding
	return nil
}))
go func() {
	_ = tui.Run(svc, "owner", input, jsOutputWriter{})
}()
select {}
```

- [ ] **Step 5: Write `wasm-bridge.js`**

```js
export async function launchAgentCommsTUI(container) {
  const { Terminal } = await import("@xterm/xterm");
  const { FitAddon } = await import("@xterm/addon-fit");
  const term = new Terminal({
    theme: {
      background: "#071216", foreground: "#D7E5E3", cursor: "#56D6C9",
      black: "#071216", brightBlack: "#78918F",
      cyan: "#56D6C9", brightCyan: "#56D6C9",
      yellow: "#E8B85C", brightYellow: "#E8B85C",
      red: "#F07167", brightRed: "#F07167",
      magenta: "#B9A7E8", brightMagenta: "#B9A7E8",
      white: "#D7E5E3", brightWhite: "#D7E5E3"
    },
    fontFamily: "var(--mono)",
    convertEol: true
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(container);
  fit.fit();

  window.agentCommsTUIOnOutput = (text) => term.write(text);
  term.onData((data) => window.agentCommsTUIWrite(new TextEncoder().encode(data)));
  term.onResize(({ cols, rows }) => window.agentCommsTUIResize(cols, rows));

  const go = new Go(); // wasm_exec.js's global, loaded separately before this module runs
  const wasm = await WebAssembly.instantiateStreaming(fetch("/tui/agent-comms-tui.wasm"), go.importObject);
  go.run(wasm.instance); // does not resolve until the Go program exits (it never does -- select{})

  window.addEventListener("resize", () => fit.fit());
  return term;
}
```

- [ ] **Step 6: Commit**

```bash
git add cmd/agent-comms-tui-wasm/wasm_main.go sites/landing/public/tui/wasm-bridge.js
git commit -m "feat(tui-wasm): wire xterm.js to the WASM TUI over syscall/js"
```

---

## Task 5: Landing site integration and build pipeline

**Files:**
- Modify: `sites/landing/src/components/ControlRoomFrame.tsx` (add the poster/launch wrapper around the existing markup — do not remove any existing JSX)
- Create: `sites/landing/src/components/LiveControlRoom.tsx` (the post-launch xterm.js container + reset control)
- Modify: `sites/landing/src/app/page.tsx` (control section: render `ControlRoomFrame` inside the launch wrapper instead of bare)
- Modify: `sites/landing/src/app/globals.css` (chrome/caption/launch-button styles, matching existing `.control-frame`/`.frame-chrome` conventions)
- Create: `sites/landing/scripts/build-tui-wasm.mjs`
- Modify: `sites/landing/package.json` (`"build"` script gains the new step; add `@xterm/xterm` + `@xterm/addon-fit` to `"dependencies"`)

**Interfaces:**
- Consumes: `launchAgentCommsTUI(container: HTMLElement)` (Task 4's `wasm-bridge.js`, dynamically imported).
- Produces: a `LiveControlRoom` component with props `{ onReset?: () => void }`, rendering a `<div>` container that `wasm-bridge.js` mounts xterm.js into, plus a reset button that reloads the WASM module fresh (simplest correct implementation: reload the `<iframe>`-free approach isn't available since it's not an iframe — instead, tear down the existing `Terminal` instance, re-fetch/re-instantiate a fresh WASM module, matching a literal page reload's effect without a real reload; if that proves complex mid-implementation, the honest fallback is a real reload of just this section's state via a full-page reload, which is still a legitimate "reset" as long as it's labeled accurately).

- [ ] **Step 1: Write `build-tui-wasm.mjs`**

```js
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, existsSync } from "node:fs";
import { resolve } from "node:path";

const repoRoot = resolve(import.meta.dirname, "..", "..", "..");
const outDir = resolve(import.meta.dirname, "..", "public", "tui");
mkdirSync(outDir, { recursive: true });

const wasmOut = resolve(outDir, "agent-comms-tui.wasm");
execFileSync("go", ["build", "-o", wasmOut, "./cmd/agent-comms-tui-wasm"], {
  cwd: repoRoot,
  env: { ...process.env, GOOS: "js", GOARCH: "wasm" },
  stdio: "inherit"
});

const goroot = execFileSync("go", ["env", "GOROOT"], { encoding: "utf8" }).trim();
const wasmExecCandidates = [
  resolve(goroot, "lib", "wasm", "wasm_exec.js"),
  resolve(goroot, "misc", "wasm", "wasm_exec.js")
];
const wasmExecSrc = wasmExecCandidates.find(existsSync);
if (!wasmExecSrc) {
  throw new Error(`wasm_exec.js not found in either of: ${wasmExecCandidates.join(", ")}`);
}
copyFileSync(wasmExecSrc, resolve(outDir, "wasm_exec.js"));
console.log(`Built agent-comms-tui.wasm -> ${wasmOut}`);
```
(The two-candidate-path lookup covers both older and newer Go toolchain layouts — confirmed this Go 1.26 installation uses `lib/wasm/wasm_exec.js`, but `misc/wasm/wasm_exec.js` was the layout in earlier Go versions and CI's pinned version should be checked, not assumed identical to the local dev machine.)

- [ ] **Step 2: Wire it into `npm run build`**

In `sites/landing/package.json`:
```json
"build": "node scripts/build-tui-wasm.mjs && next build",
```

- [ ] **Step 3: Add xterm.js dependencies**

```bash
cd sites/landing && npm install @xterm/xterm @xterm/addon-fit
```

- [ ] **Step 4: Write `LiveControlRoom.tsx`**

```tsx
"use client";

import { useEffect, useRef, useState } from "react";

type LaunchState = "idle" | "loading" | "live" | "error";

export function LiveControlRoom() {
  const containerRef = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<LaunchState>("idle");

  useEffect(() => {
    if (state !== "loading" || !containerRef.current) return;
    let cancelled = false;
    (async () => {
      try {
        await import(/* webpackIgnore: true */ "/tui/wasm_exec.js");
        const { launchAgentCommsTUI } = await import("/tui/wasm-bridge.js");
        if (cancelled || !containerRef.current) return;
        await launchAgentCommsTUI(containerRef.current);
        if (!cancelled) setState("live");
      } catch {
        if (!cancelled) setState("error");
      }
    })();
    return () => { cancelled = true; };
  }, [state]);

  if (state === "idle") {
    return (
      <button type="button" className="control-launch" onClick={() => setState("loading")}>
        Launch the Control Room <span>↗</span>
      </button>
    );
  }
  if (state === "error") {
    return <p className="control-launch-error">Couldn&rsquo;t load the live terminal. <button type="button" onClick={() => setState("loading")}>Try again</button></p>;
  }
  return (
    <div className="control-live" aria-live="polite">
      {state === "loading" && <p className="control-loading">Starting the real TUI…</p>}
      <div ref={containerRef} className="control-terminal" />
      {state === "live" && (
        <button type="button" className="control-reset" onClick={() => window.location.reload()}>
          Reset session
        </button>
      )}
    </div>
  );
}
```
(Resolves the "reset" open question from Task 5's Interfaces block with the honest fallback: a real reload of the page, clearly labeled — revisit only if a cleaner in-place re-seed proves easy once Task 3/4 are working end to end; do not spend extra time on it before that.)

- [ ] **Step 5: Wrap `ControlRoomFrame` as the poster**

In `sites/landing/src/app/page.tsx`, find the control section's existing `<ControlRoomFrame />` usage and change it to:
```tsx
<div className="control-launcher">
  <ControlRoomFrame />
  <LiveControlRoom />
</div>
```
Update the figcaption text from "RECREATED FROM THE REAL TUI" to "THE REAL TUI, SEEDED WITH A DEMO PROJECT" (find the exact current figcaption JSX and change only that text node — do not restructure the figure).

- [ ] **Step 6: Add CSS for the launch button, loading state, and terminal container**

In `sites/landing/src/app/globals.css`, add rules for `.control-launcher` (position the poster and the live terminal in the same stacking area, poster hidden once `.control-live` is present — simplest: render `ControlRoomFrame` only while `LiveControlRoom`'s internal state is `"idle"`, by lifting that state up one level instead of CSS-hiding; revise Step 5's JSX to pass `ControlRoomFrame` as a child/conditional of `LiveControlRoom` rather than a CSS overlay, since that avoids shipping two copies of the chrome at once), `.control-launch` (matches `.action--ink`/`.action--line` button styling already established sitewide), `.control-terminal` (fixed aspect ratio or min-height matching `ControlRoomFrame`'s rendered size, so there's no layout jump on launch), `.control-loading`, `.control-reset`.

- [ ] **Step 7: Run the standard build/verification cycle**

```bash
cd sites/landing
npm run check
```
Expected: succeeds, and confirm `public/tui/agent-comms-tui.wasm` + `public/tui/wasm_exec.js` exist in the build output.

- [ ] **Step 8: Commit**

```bash
git add sites/landing/
git commit -m "feat(landing): launch the real WASM TUI from the Control Room section

ControlRoomFrame becomes the pre-launch poster. A 'Launch the Control
Room' interaction lazy-loads the WASM module + xterm.js and swaps in
a live, interactive terminal in the same chrome."
```

---

## Task 6: Playwright verification of the live terminal

**Files:**
- Modify: `sites/landing/tests/landing.spec.ts` (new test(s))
- Modify: `sites/landing/tests/visual.spec.ts` (new baseline screenshot for the launched state, if this repo's convention captures one per major interactive state — check existing patterns for `.control-frame` first)

**Interfaces:**
- Consumes: the `LiveControlRoom` component's rendered DOM (`.control-launch` button, `.control-terminal` container, xterm.js's own rendered `<canvas>`/`<div class="xterm-...">` elements once live).

- [ ] **Step 1: Write the failing test**

```ts
test("launches the real TUI in the control room and can act on the seeded approval", async ({ page }) => {
  await page.goto("/");
  const controlSection = page.locator("#control");
  await controlSection.scrollIntoViewIfNeeded();
  await page.getByRole("button", { name: /Launch the Control Room/ }).click();
  const terminal = page.locator(".control-terminal");
  await expect(terminal).toBeVisible();
  // xterm.js renders into canvas/DOM rows -- wait for real seeded text to
  // actually appear, not just the container existing.
  await expect(page.getByText("AXIOM", { exact: false })).toBeVisible({ timeout: 20_000 });
  await expect(page.getByText("Approvals", { exact: false })).toBeVisible();

  // Drive a real keystroke: navigate into Approvals and confirm the
  // rendered view actually changes, proving this is live, not a screenshot.
  await terminal.click();
  await page.keyboard.press("Tab"); // confirm the real key binding for "next hub tab" by reading internal/tui/model.go's key handling before finalizing this -- do not guess the binding
  await expect(page.getByText(/PENDING/i)).toBeVisible();
});
```

- [ ] **Step 2: Run it against a local build to verify it fails first, then passes**

```bash
npm run build && npx playwright test -g "launches the real TUI" 
```
Expected: FAIL before Tasks 1-5 are complete (or, run this only after Tasks 1-5 — if run now, expected FAIL because `.control-launch` doesn't exist yet). After Tasks 1-5 land: PASS.

- [ ] **Step 3: Run the full existing suite to confirm nothing regressed**

```bash
docker run --rm -v "$(pwd)/../..:/work" -w /work -e PUBLIC_PRODUCT_VERSION=0.4.0 mcr.microsoft.com/playwright:v1.62.1-noble bash -c "git config --global --add safe.directory /work && cd sites/landing && npm run test:e2e:update"
```
Expected: full pass, run from `sites/landing` with the repo root two levels up mounted (adjust the bind-mount path if run from a different cwd than this plan assumes).

- [ ] **Step 4: Run Lighthouse**

```bash
npm run lighthouse
```
Expected: accessibility ≥ 0.96, no new findings versus this repo's established baseline (check `output/lighthouse/home-run0.json` for any new `color-contrast`/other findings outside the pre-existing `#control .tui-*` set already known and accepted).

- [ ] **Step 5: Commit**

```bash
git add sites/landing/tests/
git commit -m "test(landing): verify the live WASM TUI actually renders seeded content and responds to real input"
```

---

## Task 7: Open the PR

- [ ] **Step 1: Push the branch and open a PR against `dev`**

```bash
git push -u origin feat/interactive-control-room-tui
gh pr create --base dev --head feat/interactive-control-room-tui \
  --title "feat(landing): replace the Control Room's static recreation with the real, live TUI" \
  --body "Compiles internal/tui to WebAssembly and drives it live from sites/landing's Control Room section via xterm.js, seeded with a real demo project through the actual Service.Execute API (same AXIOM/DAMON/GORGE story ControlRoomFrame already told statically).

Includes one real-product-code change, called out separately for review: internal/daemon.New's cache parameter is now an interface (cacheStore) instead of a concrete *localcache.Cache pointer, mirroring the existing authorityClient pattern. Zero behavior change for real callers -- go build ./... and go test ./... produce identical results before and after (see the daemon-iface-*.log verification in the first commit).

🤖 Generated with [Claude Code](https://claude.com/claude-code)"
```

- [ ] **Step 2: Watch CI, do not merge**

Watch `gh pr checks <number>` until settled. Report the result. Do not merge this PR without the user's explicit go-ahead — this is real product code plus a large new feature, unlike this session's earlier small landing-only fixes.

---

## Self-Review Notes

- **Spec coverage:** every locked decision from the grill-me interview (real WASM TUI, real Execute-based seeding, curated view scope, lazy-load, poster-then-live swap, full interactivity, AXIOM/DAMON actor story) maps to a task above. The daemon interface-ification (the one deviation discovered mid-planning, resolved as "do what's best" → the low-risk interface extension) is Task 1, isolated and independently verifiable before anything else depends on it.
- **Known open technical spikes inside otherwise-concrete tasks** (flagged inline, not hidden): the exact `daemonclient`/`http.RoundTripper` bridging mechanism (Task 3 Step 2), the exact `WindowSizeEvent` wire encoding (Task 4 Step 1), and the exact key binding for hub-tab navigation (Task 6 Step 1) all require reading specific real source before the step's code can be finalized — each names exactly which file to read to resolve it, rather than guessing.
- **Type/name consistency check:** `seedDemoProject(s *service.Service) error` (Task 3) is the same signature used in its own test (Task 3 Step 5) and nowhere redeclared differently. `bootstrapDemoService() (*service.Service, error)` likewise consistent across Task 3 Steps 3, 5, and Task 4's `main()` usage. `cacheStore` (Task 1) matches the interface `wasmdemo.NewMemoryCache()`'s return type must satisfy (Task 2) — confirmed same six methods listed in both places.
