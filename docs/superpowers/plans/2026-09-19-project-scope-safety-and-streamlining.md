# Project-Scope Directory Safety and Command Streamlining Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a `projectRequired` command run outside any Agent Comms project fail with a clear, guided error instead of a raw filesystem error; stop session-cache writers from ever writing into a directory that isn't a real project; and collapse two confirmed-redundant command pairs (`update check`/`update apply`, `project upgrade status`/`plan`) into one command each.

**Architecture:** A new `NOT_A_PROJECT` classified error, checked first in `PersistentPreRunE` for `projectRequired` commands, replaces a raw filesystem error. A new small `internal/sessioncache` package centralizes "where does a project-scoped, non-authoritative cache file live" as one hashed, `identity.ConfigDir()`-rooted path function; `sessionbind`, `claudeserve`, `codexserve`, and `opencodeclient` each swap their own inline `filepath.Join(root, ".agent-comms", "cache", ...)` for a call into it. `update`'s two subcommands merge into one `RunE` that always checks first, then either prompts (interactive) or proceeds under `--yes`/`--non-interactive` (scripted) — both branches reusing the exact install logic `update apply` already has. `project upgrade status` is deleted; its one caller-visible difference from `plan` (the command name) goes with it.

**Tech Stack:** Go (this repo's existing `internal/app`, `internal/identity`, `internal/projectlifecycle`, `internal/failure` packages), `spf13/cobra`, the existing project test conventions (`t.TempDir()`, `t.Setenv`, white-box `*cli{...}` construction already used in `internal/app/user_upgrade_test.go`).

**Spec:** [docs/rfcs/0035-project-scope-safety-and-command-streamlining.md](../../rfcs/0035-project-scope-safety-and-command-streamlining.md)

## Global Constraints

- Breaking changes are acceptable and expected (pre-1.0, no deprecation aliases) — matches every prior CLI RFC in this repo.
- Every new/changed exported behavior needs a `CHANGELOG.md` entry under `[Unreleased]` (Added/Changed/Fixed/Breaking, per this repo's own convention — see any existing entry for the bullet style).
- Any test that now transitively depends on `identity.ConfigDir()` (because it calls something that now calls `sessioncache.Path`) MUST set `t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())` first, or it will read/write the real machine's `~/.config/agent-comms/` during `go test`.
- After every task: `go build $(go list ./... | grep -v cmd/agent-comms-tui-wasm | grep -v internal/contamination)`, `go vet ./...`, and the specific package's tests must pass before moving to the next task.
- `agent-comms-docgen --check` only needs to pass once, after Task 8 and Task 9 (the two tasks that change the CLI's help/flags surface) — regenerate with `go run ./cmd/agent-comms-docgen` and commit the diff as part of Task 9.
- Commit after every task, following this repo's existing commit-message style (a summary line, a body explaining root cause/rationale, ending with `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`).

---

### Task 1: `NOT_A_PROJECT` guided error, and dedupe `currentInitializedProject`

**Files:**
- Modify: `internal/projectlifecycle/types.go` (add error code)
- Modify: `internal/app/app.go` (wire the check into `PersistentPreRunE`, add the error hint)
- Modify: `internal/failure/failure.go` (exit status)
- Modify: `internal/app/cmd_update.go:149-163` (dedupe `currentInitializedProject`)
- Test: `internal/app/app_test.go` (new test)

**Interfaces:**
- Consumes: `initializedProject(root string) bool` (already exists, `internal/app/user_upgrade.go`, already fixed in commit `13d8cf0` to require `config.json`).
- Produces: `projectlifecycle.CodeNotAProject` (new `ErrorCode` constant), usable by any later task or command that needs to report "not a real project" the same way.

- [ ] **Step 1: Add the new error code**

In `internal/projectlifecycle/types.go`, add to the existing `const` block (right after `CodeConflict`):

```go
	CodeConflict           ErrorCode = "CONFLICT"
	CodeNotAProject        ErrorCode = "NOT_A_PROJECT"
```

- [ ] **Step 2: Add the exit status mapping**

In `internal/failure/failure.go`, in `ExitStatus`'s `switch`, add a case alongside the existing `CodeUpgradeRequired`/`CodeProjectTooNew`/`CodeUpgradeUnsupported` line (same exit code as plain `VALIDATION`, since this is fundamentally a usage error):

```go
	case string(controlplane.CodeValidation), string(projectlifecycle.CodeNotAProject):
		return 2
```

(This replaces the existing `case string(controlplane.CodeValidation): return 2` line — merge the two cases into one, don't duplicate the `case 2:` block.)

- [ ] **Step 3: Add the error hint**

In `internal/app/app.go`'s `errorHint` function, add a case before `default`:

```go
	case "NOT_A_PROJECT":
		return "Run `agent-comms init` here to start a new project, or run this command from an existing one."
```

- [ ] **Step 4: Write the failing test**

Add to `internal/app/app_test.go`:

```go
func TestProjectRequiredCommandOutsideProjectGetsGuidedError(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	var stdout, stderr bytes.Buffer
	err = Run([]string{"task", "list", "--json"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected an error running a projectRequired command outside any project")
	}
	var envelope Envelope
	if decodeErr := json.Unmarshal(stderr.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode error envelope: %v\nstderr: %s", decodeErr, stderr.String())
	}
	if envelope.Error == nil {
		t.Fatal("expected an error envelope")
	}
	if envelope.Error.Code != "NOT_A_PROJECT" {
		t.Fatalf("error code = %q, want NOT_A_PROJECT", envelope.Error.Code)
	}
	if !strings.Contains(envelope.Error.Message, dir) {
		t.Fatalf("expected the error message to name the directory %q, got: %s", dir, envelope.Error.Message)
	}
}
```

Check the top of `internal/app/app_test.go` for existing imports of `bytes`, `encoding/json`, `os`, `path/filepath`, `strings` — add any missing from that list (this repo's other tests in the same file already use all five, so they are very likely already imported; only add what's actually missing after checking).

- [ ] **Step 5: Run it to see it fail**

```bash
go test ./internal/app/ -run TestProjectRequiredCommandOutsideProjectGetsGuidedError -v
```

Expected: FAIL — today this command returns a `VALIDATION` error (a raw `lstat` message), not `NOT_A_PROJECT`.

- [ ] **Step 6: Wire the check into `PersistentPreRunE`**

In `internal/app/app.go`, inside `PersistentPreRunE`, find this existing block:

```go
			root := c.project
			if root == "" {
				var e error
				root, e = os.Getwd()
				if e != nil {
					return e
				}
			}
			// projectOptional commands run project-less when the current
			// directory has no initialized project. They set c.svc to nil
			// and each RunE checks for it -- see RFC 0027 section 12.
			if scope == projectOptional {
```

Insert a new check between the `root := ...` block and the `projectOptional` comment/block:

```go
			root := c.project
			if root == "" {
				var e error
				root, e = os.Getwd()
				if e != nil {
					return e
				}
			}
			if scope == projectRequired {
				if absoluteRoot, absErr := filepath.Abs(root); absErr == nil {
					root = absoluteRoot
				}
				if !initializedProject(root) {
					return &projectlifecycle.Error{
						Code:    projectlifecycle.CodeNotAProject,
						Message: fmt.Sprintf("%s is not an Agent Comms project", root),
					}
				}
			}
			// projectOptional commands run project-less when the current
			// directory has no initialized project. They set c.svc to nil
			// and each RunE checks for it -- see RFC 0027 section 12.
			if scope == projectOptional {
```

Add `"path/filepath"` to `internal/app/app.go`'s import block (it is not currently imported there — check first with `grep -n '"path/filepath"' internal/app/app.go` to confirm before adding, in case a prior task already added it).

- [ ] **Step 7: Run the test to see it pass**

```bash
go test ./internal/app/ -run TestProjectRequiredCommandOutsideProjectGetsGuidedError -v
```

Expected: PASS

- [ ] **Step 8: Dedupe `currentInitializedProject`**

In `internal/app/cmd_update.go`, replace the entire function body (currently lines 149-163):

```go
func currentInitializedProject(explicit string) (string, bool) {
	root := explicit
	if root == "" {
		root, _ = os.Getwd()
	}
	if root == "" {
		return "", false
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	info, err := os.Lstat(filepath.Join(absolute, store.Runtime))
	return absolute, err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}
```

with:

```go
// currentInitializedProject resolves explicit (or the current working
// directory, when empty) to an absolute path and reports whether it is a
// genuinely initialized project -- delegating entirely to
// initializedProject (internal/app/user_upgrade.go) so the two never drift
// out of sync the way they did before RFC 0035 (that duplicate, looser
// check was itself a second copy of the exact bug commit 13d8cf0 fixed).
func currentInitializedProject(explicit string) (string, bool) {
	root := explicit
	if root == "" {
		root, _ = os.Getwd()
	}
	if root == "" {
		return "", false
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	return absolute, initializedProject(absolute)
}
```

Since `store.Runtime` was the only use of the `store` package in this file, remove `"github.com/DhanushSantosh/AgentComms/internal/store"` from `cmd_update.go`'s import block — confirm first with `grep -c "store\." internal/app/cmd_update.go` (expect `0` after this edit; if it prints anything higher, something else in the file still needs `store` and the import must stay).

- [ ] **Step 9: Run the full app package tests**

```bash
go build $(go list ./... | grep -v cmd/agent-comms-tui-wasm | grep -v internal/contamination) && go vet ./... && go test ./internal/app/... -count=1
```

Expected: all pass. (This also exercises every existing caller of `currentInitializedProject` and `initializedProject`, confirming the dedupe didn't change behavior for any already-passing test.)

- [ ] **Step 10: Add a CHANGELOG entry**

Under `CHANGELOG.md`'s `## [Unreleased]` heading, add:

```markdown
**Fixed**
- A `projectRequired` command (e.g. `task list`, `message post`) run
  outside any Agent Comms project used to leak a raw filesystem error
  ("open .../.agent-comms/config.json: no such file or directory") with a
  misleading "--help" hint. It now fails immediately with `NOT_A_PROJECT`
  and a clear next step: run `agent-comms init` here, or run the command
  from an existing project. `currentInitializedProject` (used by `update`
  and by `projectOptional` commands) was a second, separately-drifted copy
  of the same "does a stray .agent-comms directory count as a project"
  check fixed in the underlying helper on 2026-09-18 -- it now delegates
  to that one fixed implementation instead of re-checking loosely on its
  own.
```

- [ ] **Step 11: Commit**

```bash
git add internal/projectlifecycle/types.go internal/failure/failure.go internal/app/app.go internal/app/cmd_update.go internal/app/app_test.go CHANGELOG.md
git commit -m "fix(cli): guided NOT_A_PROJECT error for projectRequired commands (RFC 0035)

A projectRequired command run outside any project (e.g. task list in an
empty directory) failed with a raw filesystem error and an irrelevant
--help hint. PersistentPreRunE now checks initializedProject(root) first
and fails fast with a clear NOT_A_PROJECT error naming the directory and
suggesting agent-comms init.

Also found and fixed a second, previously-missed copy of the exact bug
this project fixed yesterday in commit 13d8cf0: cmd_update.go's
currentInitializedProject was a separate, looser reimplementation of the
same idea (checked only that .agent-comms/ existed, not config.json). It
now delegates to the one already-fixed initializedProject instead of
duplicating -- used by update's own pre-check and by every projectOptional
command (config, doctor, agent-instructions, profile current).

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 2: New `internal/sessioncache` package

**Files:**
- Create: `internal/sessioncache/sessioncache.go`
- Test: `internal/sessioncache/sessioncache_test.go`

**Interfaces:**
- Produces: `func Path(root, kind string) (string, error)` — later tasks (3-6) call this instead of building their own `.agent-comms/cache/...` path.

- [ ] **Step 1: Write the failing test**

```go
package sessioncache

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPathNeverPointsInsideRoot(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	path, err := Path(root, "claude-serve")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(path, root) {
		t.Fatalf("Path returned a path inside root: %s (root=%s)", path, root)
	}
	if filepath.Ext(path) != ".json" {
		t.Fatalf("expected a .json path, got %s", path)
	}
}

func TestPathIsStableForTheSameRootAndKind(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	first, err := Path(root, "codex-serve")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Path(root, "codex-serve")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("Path is not stable: %q != %q", first, second)
	}
}

func TestPathDiffersByKindForTheSameRoot(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	claudePath, err := Path(root, "claude-serve")
	if err != nil {
		t.Fatal(err)
	}
	codexPath, err := Path(root, "codex-serve")
	if err != nil {
		t.Fatal(err)
	}
	if claudePath == codexPath {
		t.Fatal("expected different kinds for the same root to produce different paths")
	}
}

func TestPathDiffersByRootForTheSameKind(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	first, err := Path(t.TempDir(), "runtime-sessions")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Path(t.TempDir(), "runtime-sessions")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("expected different roots to produce different paths")
	}
}
```

- [ ] **Step 2: Run it to see it fail**

```bash
go test ./internal/sessioncache/... -v
```

Expected: FAIL to compile (`Path` undefined) — the package doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```go
// Package sessioncache locates on-disk cache files for project-scoped,
// non-authoritative session state: which live-serve broker is running for
// a project, which provider conversation a runtime is bound to. This data
// is useful across process restarts, but several callers of it (live
// serve/attach, runtime session binding) may legitimately run against a
// directory that is not, and was never meant to be, a real initialized
// Agent Comms project -- so it must never be written inside that
// directory. See RFC 0035: writing directly into
// <root>/.agent-comms/cache/*.json (the exact directory name a real
// project uses for its own managed state) is what left a stray directory
// indistinguishable from a real project, the root cause of a v0.7.0 bug
// (commit 13d8cf0).
package sessioncache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
)

// Path returns the cache file path for (root, kind). root is typically a
// project root, or any working directory a broker or session was launched
// from; kind names the cache's purpose (e.g. "claude-serve", "codex-serve",
// "opencode-server", "runtime-sessions"). The returned file lives under
// identity.ConfigDir(), never inside root itself, so root's own
// directory -- initialized project or not -- is never touched.
func Path(root, kind string) (string, error) {
	configDir, err := identity.ConfigDir()
	if err != nil {
		return "", err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(absoluteRoot))
	name := fmt.Sprintf("%s-%s.json", kind, hex.EncodeToString(sum[:8]))
	return filepath.Join(configDir, "sessions", name), nil
}
```

- [ ] **Step 4: Run the test to see it pass**

```bash
go test ./internal/sessioncache/... -v
```

Expected: PASS

- [ ] **Step 5: Build and vet**

```bash
go build ./... && go vet ./...
```

Expected: clean (confirms no import cycle between `sessioncache` and `identity`).

- [ ] **Step 6: Commit**

```bash
git add internal/sessioncache/
git commit -m "feat(sessioncache): new package for project-scoped cache paths (RFC 0035)

Path(root, kind) hashes (root, kind) into a filename under
identity.ConfigDir() -- never inside root itself, regardless of whether
root is a real initialized project. Tasks 3-6 migrate sessionbind,
claudeserve, codexserve, and opencodeclient onto this from their own
inline <root>/.agent-comms/cache/*.json construction.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 3: Migrate `sessionbind` to `sessioncache`

**Files:**
- Modify: `internal/sessionbind/sessionbind.go`
- Modify: `internal/sessionbind/sessionbind_test.go`

**Interfaces:**
- Consumes: `sessioncache.Path(root, kind string) (string, error)` (Task 2).
- Produces: `Path(projectRoot string) (string, error)` — signature change from today's `func Path(projectRoot string) string`. `Save`/`Load`'s own public signatures are unchanged (they already return `error`).

- [ ] **Step 1: Update `Path` and its two internal callers**

In `internal/sessionbind/sessionbind.go`, replace:

```go
// Path returns the local binding file path for a project root.
func Path(projectRoot string) string {
	return filepath.Join(projectRoot, ".agent-comms", "cache", "runtime-sessions.json")
}
```

with:

```go
// Path returns the local binding file path for a project root. See
// internal/sessioncache's own doc comment for why this is never inside
// projectRoot itself.
func Path(projectRoot string) (string, error) {
	return sessioncache.Path(projectRoot, "runtime-sessions")
}
```

Add `"github.com/DhanushSantosh/AgentComms/internal/sessioncache"` to the import block.

Update `Save` (currently `path := Path(projectRoot)`):

```go
func Save(projectRoot, runtimeID, sessionID, adapter string) error {
	path, err := Path(projectRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	bindings, err := load(path)
	if err != nil {
		return err
	}
	bindings[runtimeID] = Binding{SessionID: sessionID, Adapter: adapter, CapturedAt: time.Now().UTC()}
	raw, err := json.MarshalIndent(bindings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}
```

Update `Load`:

```go
func Load(projectRoot, runtimeID string) (Binding, bool, error) {
	path, err := Path(projectRoot)
	if err != nil {
		return Binding{}, false, err
	}
	bindings, err := load(path)
	if err != nil {
		return Binding{}, false, err
	}
	binding, ok := bindings[runtimeID]
	return binding, ok, nil
}
```

- [ ] **Step 2: Update the test file**

In `internal/sessionbind/sessionbind_test.go`, add `t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())` as the first line of `TestSaveAndLoadRoundTrips`, `TestLoadMissingBindingReportsNotFound`, and `TestSavePreservesOtherRuntimeBindings` (the three tests that call `Save`/`Load`; `TestCapture*` tests don't touch the filesystem and need no change).

- [ ] **Step 3: Run the package tests**

```bash
go test ./internal/sessionbind/... -v
```

Expected: PASS (all 7 existing tests, unchanged assertions — only `Path`'s internals moved).

- [ ] **Step 4: Add a regression test proving nothing writes into root**

Append to `internal/sessionbind/sessionbind_test.go`:

```go
func TestSaveNeverWritesInsideProjectRoot(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	if err := Save(root, "some-runtime", "session-id", "claude"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected Save to leave root untouched, found: %v", entries)
	}
}
```

Add `"os"` to the test file's imports if not already present.

- [ ] **Step 5: Run it**

```bash
go test ./internal/sessionbind/... -run TestSaveNeverWritesInsideProjectRoot -v
```

Expected: PASS

- [ ] **Step 6: Build and vet the whole repo**

```bash
go build $(go list ./... | grep -v cmd/agent-comms-tui-wasm | grep -v internal/contamination) && go vet ./...
```

Expected: clean (`sessionbind.Path`/`Save`/`Load` have no external callers whose call sites need updating beyond what's already in this task — `internal/app/*.go` calls `Save`/`Load`, not `Path`, and neither of those two public signatures changed).

- [ ] **Step 7: Commit**

```bash
git add internal/sessionbind/
git commit -m "fix(sessionbind): move runtime-session cache out of the working directory (RFC 0035)

Path used to return <projectRoot>/.agent-comms/cache/runtime-sessions.json
directly -- writable into any directory, project or not, which is the
confirmed origin of a v0.7.0 bug (a stray cache-only .agent-comms
directory in \$HOME, mistaken for a real project). Now delegates to
sessioncache.Path, which never writes inside the target directory.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 4: Migrate `claudeserve` to `sessioncache`

**Files:**
- Modify: `internal/claudeserve/server.go`
- Modify: `internal/claudeserve/server_test.go`

**Interfaces:**
- Consumes: `sessioncache.Path(root, kind string) (string, error)` (Task 2).
- Produces: `ServerInfoPath(projectRoot string) (string, error)` — signature change from today's `func ServerInfoPath(projectRoot string) string`.

- [ ] **Step 1: Update `ServerInfoPath`**

In `internal/claudeserve/server.go`, replace:

```go
func ServerInfoPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".agent-comms", "cache", "claude-serve.json")
}
```

with:

```go
// ServerInfoPath returns the local tracking file path for a project root.
// See internal/sessioncache's own doc comment for why this is never
// inside projectRoot itself.
func ServerInfoPath(projectRoot string) (string, error) {
	return sessioncache.Path(projectRoot, "claude-serve")
}
```

Add `"github.com/DhanushSantosh/AgentComms/internal/sessioncache"` to the import block.

- [ ] **Step 2: Update `EnsureServer`, the one internal caller**

Replace:

```go
func EnsureServer(ctx context.Context, projectRoot, workDir string) (string, error) {
	path := ServerInfoPath(projectRoot)
```

with:

```go
func EnsureServer(ctx context.Context, projectRoot, workDir string) (string, error) {
	path, err := ServerInfoPath(projectRoot)
	if err != nil {
		return "", err
	}
```

(The rest of `EnsureServer`'s body is unchanged — it already declares `baseURL, err := spawnServer(...)` further down, so confirm this new `err` declaration doesn't collide; if `go vet`/`go build` reports a redeclaration, rename this one to `pathErr` instead and adjust its `if` check accordingly.)

- [ ] **Step 3: Update the test file's four call sites**

In `internal/claudeserve/server_test.go`, each of the 4 lines matching `ServerInfoPath(t.TempDir())` needs its enclosing test to add `t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())` as its first line (if not already present in that test function), and the call site itself becomes:

```go
path, err := ServerInfoPath(t.TempDir())
if err != nil {
	t.Fatal(err)
}
```

Read the file first (`internal/claudeserve/server_test.go`) to see each of the 4 exact surrounding test functions before editing, since the right-hand-side usage of `path` differs per test (some pass it directly into `resolveRunningServer`, in which case wrap that in the same pattern: compute `path, err := ServerInfoPath(t.TempDir())` first, check `err`, then use `path` where the inline call used to be).

- [ ] **Step 4: Run the package tests**

```bash
go test ./internal/claudeserve/... -v
```

Expected: PASS

- [ ] **Step 5: Add a regression test proving nothing writes into root**

Append to `internal/claudeserve/server_test.go`:

```go
func TestServerInfoPathNeverPointsInsideProjectRoot(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	path, err := ServerInfoPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(path, root) {
		t.Fatalf("ServerInfoPath returned a path inside root: %s", path)
	}
}
```

Add `"strings"` to the test file's imports if not already present.

- [ ] **Step 6: Run it, then build/vet the whole repo**

```bash
go test ./internal/claudeserve/... -run TestServerInfoPathNeverPointsInsideProjectRoot -v
go build $(go list ./... | grep -v cmd/agent-comms-tui-wasm | grep -v internal/contamination) && go vet ./...
```

Expected: both clean.

- [ ] **Step 7: Commit**

```bash
git add internal/claudeserve/
git commit -m "fix(claudeserve): move live-serve cache out of the working directory (RFC 0035)

Same fix as sessionbind (this commit's sibling): ServerInfoPath now
delegates to sessioncache.Path instead of writing
<projectRoot>/.agent-comms/cache/claude-serve.json directly.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 5: Migrate `codexserve` to `sessioncache`

**Files:**
- Modify: `internal/codexserve/server.go`
- Modify: `internal/codexserve/server_test.go`

**Interfaces:**
- Consumes: `sessioncache.Path(root, kind string) (string, error)` (Task 2).
- Produces: `ServerInfoPath(projectRoot string) (string, error)`.

This task is identical in shape to Task 4, applied to `codexserve` instead of `claudeserve` — confirmed earlier in the design phase to have exactly the same code structure (`ServerInfoPath`, `EnsureServer`, and the 4 test call sites at `internal/codexserve/server_test.go` lines 28, 42, 54, 61).

- [ ] **Step 1: Update `ServerInfoPath`**

In `internal/codexserve/server.go`, replace:

```go
func ServerInfoPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".agent-comms", "cache", "codex-serve.json")
}
```

with:

```go
// ServerInfoPath returns the local tracking file path for a project root.
// See internal/sessioncache's own doc comment for why this is never
// inside projectRoot itself.
func ServerInfoPath(projectRoot string) (string, error) {
	return sessioncache.Path(projectRoot, "codex-serve")
}
```

Add `"github.com/DhanushSantosh/AgentComms/internal/sessioncache"` to the import block.

- [ ] **Step 2: Update `EnsureServer`**

Same pattern as Task 4 Step 2: replace `path := ServerInfoPath(projectRoot)` with a `path, err := ServerInfoPath(projectRoot)` plus an `if err != nil { return "", err }` guard, renaming to `pathErr` instead if `err` is already declared later in the same function.

- [ ] **Step 3: Update the 4 test call sites**

Same pattern as Task 4 Step 3, applied to `internal/codexserve/server_test.go`.

- [ ] **Step 4: Add the regression test**

```go
func TestServerInfoPathNeverPointsInsideProjectRoot(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	path, err := ServerInfoPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(path, root) {
		t.Fatalf("ServerInfoPath returned a path inside root: %s", path)
	}
}
```

- [ ] **Step 5: Run tests, then build/vet**

```bash
go test ./internal/codexserve/... -v
go build $(go list ./... | grep -v cmd/agent-comms-tui-wasm | grep -v internal/contamination) && go vet ./...
```

Expected: both clean.

- [ ] **Step 6: Commit**

```bash
git add internal/codexserve/
git commit -m "fix(codexserve): move live-serve cache out of the working directory (RFC 0035)

Same fix as claudeserve and sessionbind: ServerInfoPath now delegates to
sessioncache.Path instead of writing
<projectRoot>/.agent-comms/cache/codex-serve.json directly.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 6: Migrate `opencodeclient` to `sessioncache`

**Files:**
- Modify: `internal/opencodeclient/server.go`
- Modify: `internal/opencodeclient/server_test.go`

**Interfaces:**
- Consumes: `sessioncache.Path(root, kind string) (string, error)` (Task 2).
- Produces: `ServerInfoPath(projectRoot string) (string, error)`.

- [ ] **Step 1: Update `ServerInfoPath`**

In `internal/opencodeclient/server.go`, replace:

```go
// ServerInfoPath returns the local tracking file path for a project root.
func ServerInfoPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".agent-comms", "cache", "opencode-server.json")
}
```

with:

```go
// ServerInfoPath returns the local tracking file path for a project root.
// See internal/sessioncache's own doc comment for why this is never
// inside projectRoot itself.
func ServerInfoPath(projectRoot string) (string, error) {
	return sessioncache.Path(projectRoot, "opencode-server")
}
```

Add `"github.com/DhanushSantosh/AgentComms/internal/sessioncache"` to the import block.

- [ ] **Step 2: Update `EnsureServer`**

Same pattern as Task 4 Step 2, applied here: replace `path := ServerInfoPath(projectRoot)` with `path, err := ServerInfoPath(projectRoot)` plus a guard, renaming to `pathErr` if `err` collides with a later declaration in the same function body.

- [ ] **Step 3: Update the 5 test call sites**

`internal/opencodeclient/server_test.go` has 5 occurrences (lines 35, 49, 70, 87, 95, per the earlier grep) — same pattern as Task 4 Step 3: read the file first, add `t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())` to each affected test, adapt each call site to handle the new `(string, error)` return.

- [ ] **Step 4: Add the regression test**

```go
func TestServerInfoPathNeverPointsInsideProjectRoot(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	path, err := ServerInfoPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(path, root) {
		t.Fatalf("ServerInfoPath returned a path inside root: %s", path)
	}
}
```

- [ ] **Step 5: Run tests, then build/vet**

```bash
go test ./internal/opencodeclient/... -v
go build $(go list ./... | grep -v cmd/agent-comms-tui-wasm | grep -v internal/contamination) && go vet ./...
```

Expected: both clean.

- [ ] **Step 6: Commit**

```bash
git add internal/opencodeclient/
git commit -m "fix(opencodeclient): move live-serve cache out of the working directory (RFC 0035)

Same fix as claudeserve, codexserve, and sessionbind: ServerInfoPath now
delegates to sessioncache.Path instead of writing
<projectRoot>/.agent-comms/cache/opencode-server.json directly.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 7: Merge `update check` + `update apply` into one interactive `update`

**Files:**
- Modify: `internal/app/app.go` (add injectable fields to `cli` struct)
- Modify: `internal/app/cmd_update.go` (replace the two subcommands with one)
- Test: `internal/app/cmd_update_test.go` (new file)

**Interfaces:**
- Consumes: `fetchRelease(ctx, channel, version string) (githubRelease, error)`, `installRelease(ctx, githubRelease) (map[string]any, error)`, `c.handoffProjectUpgrade(...)` (all pre-existing in `cmd_update.go`, unchanged).
- Produces: three new `cli` struct fields (`in io.Reader`, `fetchReleaseFn func(...)`, `installReleaseFn func(...)`) later tasks don't need but which make this task's own interactive path directly testable in-process, matching the existing `handoffRunner` injection pattern.

- [ ] **Step 1: Add the injectable fields**

In `internal/app/app.go`, add three fields to the `cli` struct (after the existing `handoffRunner` field):

```go
type cli struct {
	out, err                                    io.Writer
	json, jsonl, nonInteractive, noColor, quiet bool
	verbose, details                            bool
	output                                      string
	project, profile, actor                     string
	timeout                                     time.Duration
	svc                                         *service.Service
	cmd                                         string
	actorResolution                             identity.ActorResolution
	pendingWarnings                             []string
	processExitCode                             int
	handoffRunner                               commandRunner
	in                                           io.Reader
	fetchReleaseFn                               func(ctx context.Context, channel, version string) (githubRelease, error)
	installReleaseFn                             func(ctx context.Context, r githubRelease) (map[string]any, error)
}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/app/cmd_update_test.go`:

```go
package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func fakeRelease(tag string) githubRelease {
	return githubRelease{Tag: tag}
}

func TestUpdatePromptsAndInstallsOnYes(t *testing.T) {
	Version = "0.7.0"
	t.Cleanup(func() { Version = "0.7.0" })

	installed := false
	c := &cli{
		out: &bytes.Buffer{}, err: &bytes.Buffer{}, timeout: time.Second,
		in: strings.NewReader("y\n"),
		fetchReleaseFn: func(ctx context.Context, channel, version string) (githubRelease, error) {
			return fakeRelease("v0.7.1"), nil
		},
		installReleaseFn: func(ctx context.Context, r githubRelease) (map[string]any, error) {
			installed = true
			return map[string]any{"version": "0.7.1", "installed": "/fake/path", "previous": "/fake/path", "verified": true}, nil
		},
	}
	root := c.root()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"update", "--skip-project-upgrade"})
	if err := root.Execute(); err != nil {
		t.Fatalf("update: %v\nstderr: %s", err, stderr.String())
	}
	if !installed {
		t.Fatal("expected the release to be installed after answering y")
	}
	if !strings.Contains(stdout.String(), "0.7.1") {
		t.Fatalf("expected stdout to mention the new version, got: %s", stdout.String())
	}
}

func TestUpdatePromptsAndSkipsInstallOnNo(t *testing.T) {
	Version = "0.7.0"
	t.Cleanup(func() { Version = "0.7.0" })

	installed := false
	c := &cli{
		out: &bytes.Buffer{}, err: &bytes.Buffer{}, timeout: time.Second,
		in: strings.NewReader("n\n"),
		fetchReleaseFn: func(ctx context.Context, channel, version string) (githubRelease, error) {
			return fakeRelease("v0.7.1"), nil
		},
		installReleaseFn: func(ctx context.Context, r githubRelease) (map[string]any, error) {
			installed = true
			return nil, nil
		},
	}
	root := c.root()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"update", "--skip-project-upgrade"})
	if err := root.Execute(); err != nil {
		t.Fatalf("update: %v\nstderr: %s", err, stderr.String())
	}
	if installed {
		t.Fatal("expected no install after answering n")
	}
}

func TestUpdateNonInteractiveInstallsWithoutPrompting(t *testing.T) {
	Version = "0.7.0"
	t.Cleanup(func() { Version = "0.7.0" })

	// c.nonInteractive is NOT set directly in the struct literal here: the
	// root command's own --non-interactive flag is bound with
	// f.BoolVar(&c.nonInteractive, "non-interactive", false, ...)
	// (internal/app/app.go), and BoolVar resets the bound variable to its
	// given default (false) the moment c.root() registers it -- so a
	// pre-set `nonInteractive: true` here would be silently overwritten
	// back to false before RunE ever runs. Passing the real flag in
	// SetArgs is the only way this field ends up true for this test.
	installed := false
	c := &cli{
		out: &bytes.Buffer{}, err: &bytes.Buffer{}, timeout: time.Second,
		fetchReleaseFn: func(ctx context.Context, channel, version string) (githubRelease, error) {
			return fakeRelease("v0.7.1"), nil
		},
		installReleaseFn: func(ctx context.Context, r githubRelease) (map[string]any, error) {
			installed = true
			return map[string]any{"version": "0.7.1", "installed": "/fake/path", "previous": "/fake/path", "verified": true}, nil
		},
	}
	root := c.root()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"update", "--skip-project-upgrade", "--non-interactive"})
	if err := root.Execute(); err != nil {
		t.Fatalf("update: %v\nstderr: %s", err, stderr.String())
	}
	if !installed {
		t.Fatal("expected --non-interactive to install without a prompt")
	}
}

func TestUpdateReportsAlreadyCurrentWithoutPrompting(t *testing.T) {
	Version = "0.7.1"
	t.Cleanup(func() { Version = "0.7.0" })

	installed := false
	c := &cli{
		out: &bytes.Buffer{}, err: &bytes.Buffer{}, timeout: time.Second,
		in: strings.NewReader(""),
		fetchReleaseFn: func(ctx context.Context, channel, version string) (githubRelease, error) {
			return fakeRelease("v0.7.1"), nil
		},
		installReleaseFn: func(ctx context.Context, r githubRelease) (map[string]any, error) {
			installed = true
			return nil, nil
		},
	}
	root := c.root()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"update", "--skip-project-upgrade"})
	if err := root.Execute(); err != nil {
		t.Fatalf("update: %v\nstderr: %s", err, stderr.String())
	}
	if installed {
		t.Fatal("expected no install when already current")
	}
	if !strings.Contains(stdout.String(), "up to date") {
		t.Fatalf("expected stdout to say up to date, got: %s", stdout.String())
	}
}
```

- [ ] **Step 3: Run the tests to see them fail**

```bash
go test ./internal/app/ -run TestUpdate -v
```

Expected: FAIL to compile — `update apply`/`update check` still exist as separate subcommands; there is no single `update` RunE yet, and `fetchReleaseFn`/`installReleaseFn`/`in` aren't consulted by anything yet.

- [ ] **Step 4: Replace the two subcommands with one**

In `internal/app/cmd_update.go`, replace the entire `updateCmd` function (from `func (c *cli) updateCmd() *cobra.Command {` through its matching closing `}`, i.e. everything currently defining `check`, `apply`, and their flags) with:

```go
func (c *cli) updateCmd() *cobra.Command {
	var channel, version string
	var yes, currentProjectOnly, skipProjectUpgrade bool
	allKnown := true
	update := &cobra.Command{
		Use:   "update",
		Short: "Check for and install a verified Agent Comms release",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			fetch := c.fetchReleaseFn
			if fetch == nil {
				fetch = fetchRelease
			}
			release, err := fetch(ctx, channel, version)
			if err != nil {
				return err
			}
			latest := strings.TrimPrefix(release.Tag, "v")
			if latest == Version {
				return c.emitDocument("update.check", map[string]any{
					"current": Version, "latest": release.Tag, "channel": channel, "update_available": false,
				}, cliui.Document{
					Title: "Agent Comms is up to date", Status: cliui.StatusSuccess,
					Fields: []cliui.Field{{Label: "Version", Value: Version}, {Label: "Channel", Value: channel}},
				})
			}
			if !yes && !c.nonInteractive {
				in := c.in
				if in == nil {
					in = os.Stdin
				}
				fmt.Fprintf(c.out, "Update available: v%s -> %s. Install? [y/N] ", Version, release.Tag)
				scanner := bufio.NewScanner(in)
				if !scanner.Scan() || !strings.EqualFold(strings.TrimSpace(scanner.Text()), "y") {
					return c.emitDocument("update.check", map[string]any{
						"current": Version, "latest": release.Tag, "channel": channel, "update_available": true, "installed": false,
					}, cliui.Document{
						Title: "Update available", Status: cliui.StatusInfo,
						Fields: []cliui.Field{{Label: "Current", Value: Version}, {Label: "Latest", Value: release.Tag}},
						Hint:   "Run agent-comms update again and answer y, or pass --yes, to install.",
					})
				}
			}
			progress := c.progress()
			_ = progress.Start("Applying Agent Comms update")
			completed := false
			defer func() {
				if !completed {
					_ = progress.Stop(false, "Update did not complete")
				}
			}()
			install := c.installReleaseFn
			if install == nil {
				install = installRelease
			}
			result, err := install(ctx, release)
			if err != nil {
				return err
			}
			result["binary_updated"] = true
			if skipProjectUpgrade {
				result["project_upgrade"] = map[string]any{"skipped": true, "reason": "requested by --skip-project-upgrade"}
				completed = true
				_ = progress.Stop(true, "Update installed")
				return c.emitUpdateApply(result)
			}
			projectRoot, projectFound := currentInitializedProject(c.project)
			effectiveAllKnown := allKnown
			if currentProjectOnly {
				effectiveAllKnown = false
			}
			knownRoots, rootsErr := c.knownProjectRoots(projectRoot)
			if rootsErr != nil {
				return rootsErr
			}
			if effectiveAllKnown && len(knownRoots) == 0 {
				result["project_upgrade"] = map[string]any{"skipped": true, "reason": "no initialized projects are registered"}
			} else if effectiveAllKnown || projectFound {
				upgradeResult, upgradeErr := c.handoffProjectUpgrade(ctx, result["installed"].(string), projectRoot, yes, effectiveAllKnown)
				if upgradeErr != nil {
					details := map[string]any{
						"binary_updated":    true,
						"installed_version": result["version"],
						"previous_version":  result["previous"],
					}
					var lifecycleErr *projectlifecycle.Error
					if errors.As(upgradeErr, &lifecycleErr) {
						lifecycleErr.Details = details
						return lifecycleErr
					}
					return &projectlifecycle.Error{
						Code:    projectlifecycle.CodeUpgradeFailed,
						Message: "binary updated successfully but project reconciliation failed: " + upgradeErr.Error(),
						Details: details,
					}
				}
				result["project_upgrade"] = upgradeResult
			} else {
				result["project_upgrade"] = map[string]any{"skipped": true, "reason": "current directory is not an initialized project"}
			}
			completed = true
			_ = progress.Stop(true, "Update and project reconciliation completed")
			return c.emitUpdateApply(result)
		},
	}
	update.Flags().StringVar(&channel, "channel", "stable", "stable or preview")
	update.Flags().StringVar(&version, "version", "", "exact release tag")
	update.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt and approve confirmation-required project migrations")
	update.Flags().BoolVar(&allKnown, "all-known", true, "reconcile projects recorded in identity profiles")
	update.Flags().BoolVar(&currentProjectOnly, "current-project-only", false, "reconcile only the current initialized project")
	update.Flags().BoolVar(&skipProjectUpgrade, "skip-project-upgrade", false, "install the binary without reconciling projects")
	return update
}
```

Notes for this step:
- `os` and `bufio` must be imported in `cmd_update.go` — check with `grep -n '"os"\|"bufio"' internal/app/cmd_update.go` first; `os` is almost certainly already imported (17 uses per Task 1's own grep), `bufio` is likely new and needs adding.
- The `--json` case (`update --json`) reuses `emitDocument`/`emitUpdateApply` exactly as `check`/`apply` did before — no separate JSON branch needed, since a non-interactive, non-TTY invocation with `--json` should also pass `c.nonInteractive` in practice (confirm this is really true by checking how `--json` and `--non-interactive` interact elsewhere in this file before relying on it silently; if `--json` alone does not already imply non-interactive today, add `|| c.json` to the prompt-skip condition: `if !yes && !c.nonInteractive && !c.json {`).
- This removes the standalone `check` and `apply` `*cobra.Command` variables entirely — search the rest of the file (and the whole `internal/app` package) for `"check"` and `"apply"` string literals or any other reference to the old two-subcommand shape before deleting, to make sure nothing else (help text, docs generation, another test) still expects them.

- [ ] **Step 5: Fix `classifyProjectScope`'s now-stale "update" exemption**

`internal/app/app.go`'s `classifyProjectScope` has a case that predates this task:

```go
	case name == "version", name == "init", name == "completion",
		name == "update" && cmd.Parent() == cmd.Root(),
		strings.HasPrefix(path, "agent-comms project upgrade"),
```

That `name == "update" && cmd.Parent() == cmd.Root()` clause exists only because, before this task, "update" was a bare parent command with no `RunE` of its own (just the "check"/"apply" children) — it made a no-subcommand invocation inert. This task turns "update" into a real leaf command with its own `RunE`, still named "update", still a direct child of root, so it still matches that clause unchanged — which would silently classify it `projectExempt` instead of `projectUserOnly` (what `update check` and `update apply` both correctly had), silently dropping the `reconcileUserInstallation` call `PersistentPreRunE` makes for `projectUserOnly` commands. Fix both cases:

```go
	case name == "version", name == "init", name == "completion",
		strings.HasPrefix(path, "agent-comms project upgrade"),
		path == "agent-comms daemon serve",
		path == "agent-comms live serve", path == "agent-comms live attach",
		path == "agent-comms runtime verify-adapter":
		return projectExempt
	case path == "agent-comms update",
		path == "agent-comms profile list", path == "agent-comms profile use",
		path == "agent-comms config theme":
		return projectUserOnly
```

(This replaces both the `projectExempt` case's `name == "update" && cmd.Parent() == cmd.Root(),` line, removing it, and the `projectUserOnly` case's `path == "agent-comms update check", path == "agent-comms update apply",` line, replacing it with `path == "agent-comms update",` — the rest of both cases is unchanged.)

- [ ] **Step 6: Run the new tests**

```bash
go test ./internal/app/ -run TestUpdate -v
```

Expected: PASS

- [ ] **Step 7: Run the full app package test suite**

```bash
go build $(go list ./... | grep -v cmd/agent-comms-tui-wasm | grep -v internal/contamination) && go vet ./... && go test ./internal/app/... -count=1
```

Expected: all pass. Pay particular attention to any pre-existing test that referenced `update check`/`update apply` by name (search `grep -rn '"check"\|"apply"' internal/app/*_test.go` before this step to know what to expect) — fix any that broke by updating them to invoke `update` instead, preserving their original assertions about the underlying behavior.

- [ ] **Step 8: Add a CHANGELOG entry**

Under `CHANGELOG.md`'s `## [Unreleased]`, in the `**Fixed**` section added by Task 1 (append to the same section rather than creating a new one), add:

```markdown
- **Breaking:** `update check` and `update apply` are now one command,
  `update`: it always checks first, then prompts to install when a newer
  release exists (`Update available: vX -> vY. Install? [y/N]`), or
  installs immediately under `--yes`/`--non-interactive` for scripts. See
  [RFC 0035](docs/rfcs/0035-project-scope-safety-and-command-streamlining.md).
```

- [ ] **Step 9: Commit**

```bash
git add internal/app/app.go internal/app/cmd_update.go internal/app/cmd_update_test.go CHANGELOG.md
git commit -m "feat(cli)!: merge update check and update apply into one interactive update (RFC 0035)

update now always checks for a newer release first, then either prompts
to install it (interactive) or installs immediately under
--yes/--non-interactive (scripts, CI) -- matching what a human always
wanted from these two steps anyway. --channel/--version/--all-known/
--current-project-only/--skip-project-upgrade all carry over unchanged.

Added cli.in/fetchReleaseFn/installReleaseFn injection points (matching
the existing handoffRunner pattern) so the interactive prompt itself has
real test coverage -- update apply/check never had direct tests before
this, since fetchRelease/installRelease had no injection point to mock
against.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 8: Merge `project upgrade status` into `plan`

**Files:**
- Modify: `internal/app/cmd_core.go`
- Test: `internal/app/cmd_core_test.go` (new file, if one doesn't already exist — check first with `ls internal/app/cmd_core_test.go`)

**Interfaces:**
- No new interfaces — this only removes a redundant command name.

- [ ] **Step 1: Write the failing test**

If `internal/app/cmd_core_test.go` doesn't exist, create it; otherwise append to it:

```go
package app

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestProjectUpgradeStatusNoLongerExists(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	project := t.TempDir()
	var stdout, stderr bytes.Buffer
	run := func(args ...string) error {
		stdout.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json", "--quiet")
		return Run(args, &stdout, &stderr)
	}
	if err := run("init", "--non-interactive", "--owner", "owner", "--mode", "personal"); err != nil {
		t.Fatalf("init: %v\n%s", err, stderr.String())
	}
	if err := run("project", "upgrade", "status"); err == nil {
		t.Fatal("expected `project upgrade status` to no longer exist")
	}
	if err := run("project", "upgrade", "plan"); err != nil {
		t.Fatalf("project upgrade plan: %v\n%s", err, stderr.String())
	}
}
```

- [ ] **Step 2: Run it to see it fail**

```bash
go test ./internal/app/ -run TestProjectUpgradeStatusNoLongerExists -v
```

Expected: FAIL — `project upgrade status` still exists today.

- [ ] **Step 3: Remove the redundant name**

In `internal/app/cmd_core.go`, find:

```go
	for _, operation := range []string{"status", "plan"} {
```

Replace with:

```go
	for _, operation := range []string{"plan"} {
```

(Leave the rest of the loop body untouched — with only one entry, this is now equivalent to inlining the loop, but keeping it as a one-element loop is the smallest possible diff and the resulting `Use: operation` / `"project.upgrade." + operation` naming stays exactly as it already is for `plan`. Do not rename `operation` to something else or unroll the loop — this task removes a redundant command name, nothing else.)

- [ ] **Step 4: Run the test to see it pass**

```bash
go test ./internal/app/ -run TestProjectUpgradeStatusNoLongerExists -v
```

Expected: PASS

- [ ] **Step 5: Run the full app package test suite**

```bash
go build $(go list ./... | grep -v cmd/agent-comms-tui-wasm | grep -v internal/contamination) && go vet ./... && go test ./internal/app/... -count=1
```

Expected: all pass.

- [ ] **Step 6: Add a CHANGELOG entry**

Append to the same `**Fixed**` section (under the `update` bullet added in Task 7):

```markdown
- **Breaking:** `project upgrade status` is removed; it was byte-for-byte
  the same code as `project upgrade plan` under a second name. Use
  `project upgrade plan`.
```

- [ ] **Step 7: Regenerate the CLI reference and verify docgen**

```bash
go run ./cmd/agent-comms-docgen
go run ./cmd/agent-comms-docgen --check
```

Expected: the second command exits 0. Review the diff to `sites/docs/src/generated/reference.json` — it should show `update check`/`update apply`/`project upgrade status` removed and the new single `update` command's flags/summary added, nothing else.

- [ ] **Step 8: Commit**

```bash
git add internal/app/cmd_core.go internal/app/cmd_core_test.go CHANGELOG.md sites/docs/src/generated/reference.json
git commit -m "feat(cli)!: remove project upgrade status, a byte-for-byte duplicate of plan (RFC 0035)

status and plan were the exact same generated command, registered under
two names with no behavioral difference. plan is kept as the more
conventional name for a dry-run view.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 9: Final verification and RFC status update

**Files:**
- Modify: `docs/rfcs/0035-project-scope-safety-and-command-streamlining.md` (Status section)

**Interfaces:** None — this task only verifies and records completion.

- [ ] **Step 1: Full fresh test suite**

```bash
go test ./... -count=1
```

Expected: all packages `ok` (41+ packages, matching the count from before this plan started — confirm no package count regressed, e.g. from a typo'd import path).

- [ ] **Step 2: Full build and vet**

```bash
go build $(go list ./... | grep -v cmd/agent-comms-tui-wasm | grep -v internal/contamination) && go vet ./...
GOOS=js GOARCH=wasm go build ./cmd/agent-comms-tui-wasm/...
```

Expected: both clean.

- [ ] **Step 3: docgen check**

```bash
go run ./cmd/agent-comms-docgen --check
```

Expected: clean (already regenerated in Task 8, this just re-confirms nothing since drifted).

- [ ] **Step 4: Real-binary smoke test**

```bash
go build -o /tmp/agc-rfc0035-smoke ./cmd/agent-comms
mkdir -p /tmp/agc-rfc0035-smoke-nonproj && cd /tmp/agc-rfc0035-smoke-nonproj
/tmp/agc-rfc0035-smoke task list 2>&1; echo "exit: $?"
```

Expected: a clean `NOT_A_PROJECT` error naming `/tmp/agc-rfc0035-smoke-nonproj`, not a raw filesystem error. Then:

```bash
mkdir -p /tmp/agc-rfc0035-smoke-project && cd /tmp/agc-rfc0035-smoke-project
/tmp/agc-rfc0035-smoke init --non-interactive --owner smoke --mode personal
/tmp/agc-rfc0035-smoke update --channel stable 2>&1
```

Expected: `update` checks and reports the current install (this locally-built dev binary has no real published tag to compare against cleanly — the exact message doesn't matter here, only that it runs without crashing and does not create anything under `/tmp/agc-rfc0035-smoke-project/.agent-comms/cache/`).

```bash
ls /tmp/agc-rfc0035-smoke-project/.agent-comms/cache/ 2>&1
```

Expected: either the directory doesn't exist, or (if this project's own daemon/other machinery created it for unrelated reasons) it does not contain `claude-serve.json`, `codex-serve.json`, `opencode-server.json`, or `runtime-sessions.json` — those four now live under `identity.ConfigDir()`, confirm with:

```bash
find "${XDG_CONFIG_HOME:-$HOME/.config}/agent-comms/sessions/" 2>&1
```

Clean up:

```bash
rm -rf /tmp/agc-rfc0035-smoke /tmp/agc-rfc0035-smoke-nonproj /tmp/agc-rfc0035-smoke-project
```

- [ ] **Step 5: Update the RFC's Status section**

In `docs/rfcs/0035-project-scope-safety-and-command-streamlining.md`, change the first line of `## Status` from:

```markdown
**Accepted, 2026-09-19.** The project owner requested this directly after a
```

to:

```markdown
**Implemented, 2026-09-19.** The project owner requested this directly after a
```

(Leave the rest of that paragraph and the whole document otherwise unchanged — this is a one-word status update, matching how every other implemented RFC in this repo records completion, e.g. RFC 0027's own `**Implemented, 2026-09-02.**` opening.)

- [ ] **Step 6: Commit**

```bash
git add docs/rfcs/0035-project-scope-safety-and-command-streamlining.md
git commit -m "docs(rfc): mark RFC 0035 implemented

All nine tasks landed: NOT_A_PROJECT guided error, currentInitializedProject
dedupe, the new sessioncache package, all four cache-writer migrations
(sessionbind, claudeserve, codexserve, opencodeclient), the update
check/apply merge, and the project upgrade status/plan merge. Full go
test ./..., go vet, docgen --check, and a real-binary smoke test all pass.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

## Self-Review Notes

- **Spec coverage:** RFC 0035's four numbered design items map onto this plan as: item 1 (guided error) -> Task 1; item 2 (cache relocation) -> Tasks 2-6; item 3 (`update` merge) -> Task 7; item 4 (`project upgrade plan`/`status` merge) -> Task 8. The RFC's own "Test and rollout plan" section's five bullets are each covered by a task's own test steps (guided-error regression test: Task 1 Step 4; stray-directory-not-a-crash: already covered by yesterday's commit `13d8cf0`, this plan's Task 1 additionally covers the CLI-level guided-error path on top of that unit-level fix; cache-relocation-never-writes-into-root: Tasks 3-6 Step 4/5 in each; `update` interactive/non-interactive/already-current: Task 7 Step 2's four tests; `project upgrade plan` consolidation: Task 8).
- **Type consistency:** `sessioncache.Path(root, kind string) (string, error)` is the one signature introduced in Task 2 and consumed identically by name in Tasks 3-6 (`sessionbind.Path`, `claudeserve.ServerInfoPath`, `codexserve.ServerInfoPath`, `opencodeclient.ServerInfoPath` — each keeps its own existing exported name, only the return type gains `, error`). `cli.fetchReleaseFn`/`installReleaseFn`/`in` (Task 7) are used only within `cmd_update.go`'s own `RunE` and `cmd_update_test.go` — no other task reads or writes them.
- **Unresolved RFC question**: "whether other, less obvious writers into `<workDir>/.agent-comms/` exist beyond the four identified here" is addressed by Task 9 Step 1's full test suite run plus the real-binary smoke test's explicit check of `identity.ConfigDir()/sessions/` — if a fifth writer existed and were missed, either the full test suite or the smoke test's directory listing would surface a stray file at the old location.
