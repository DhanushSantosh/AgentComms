# RFC 0034: Remove `agent-comms live tail`

## Status

**Accepted, 2026-09-06.** The project owner approved this removal (relayed
and independently reviewed by codex-main) before implementation began, per
`docs/rfcs/README.md`.

This removes a public CLI command, so it requires review.

## Problem and desired outcome

`agent-comms live tail --provider claude --session <id>` reads a Claude
Code session's transcript directly off disk
(`<claudeHome>/projects/<project-slug>/<sessionID>.jsonl`, an internal,
undocumented file Claude Code happens to append to -- `claudetail.go`'s
own doc comment already says as much: "this is Claude Code's internal
transcript format, not a documented, stable contract") and either replays
it from the start or follows new appends via `fsnotify`, entirely outside
Agent Comms' own runtime/broker model. It only ever worked for
`--provider claude`; `--provider codex` was always a hard error (RFC
0027's own CLI reference already documents this).

`agent-comms live attach --provider <claude|codex> --runtime <id>` is the
supported, provider-agnostic way to watch a live agent session: it
subscribes to that provider's local broker (`live serve`), which every
worker adapter and interactive session already registers with. `tail`
predates `attach`+`serve`'s broker model (RFC 0008) and was never ported
to it -- it is a second, parallel, file-system-based mechanism for a
narrower slice of the same job (Claude only, and only when you already
know a raw Claude Code session ID and its working directory by hand,
rather than an Agent Comms runtime ID).

Desired outcome: one supported way to watch a live agent session
(`live attach`, backed by `live serve`), not two.

## Proposed design

1. **CLI:** delete the `tail` sub-command of `live` (`internal/app/cmd_misc.go`'s
   `liveCmd`) and its flags (`--session`, `--project-dir`, `--no-replay`;
   `--provider` stays, `serve`/`attach` still use it).
2. **`claudetail` package:** delete `SessionPath` (a thin wrapper around
   `claudepath.SessionPath` -- see below), `Tail`, and `drain`, along with
   the `fsnotify`/file-watching machinery they alone used. **Keep**
   `Format` and its supporting `transcriptEntry`/`contentBlock` types:
   `live attach --provider claude` already calls `claudetail.Format` to
   render each event from the broker's own stream
   (`internal/app/cmd_misc.go`'s `attach` RunE), so the package survives,
   slimmed to exactly what `attach` needs.
3. **`claudepath.SessionPath`:** unaffected. It is `claudetail.SessionPath`'s
   real implementation, but it is *also* called directly by
   `internal/claudeserve/process.go` (the live broker's own session
   resolution) and exercised by `internal/worker/worker_test.go`. Only
   `claudetail`'s thin re-export goes away; the shared implementation in
   `internal/claudepath` stays exactly as-is for those real callers.
4. **Tests:** remove `TestSessionPathMatchesRealClaudeCodeLayout`,
   `TestTailReplaysExistingHistoryThenFollowsAppends`,
   `TestTailWithoutReplaySkipsExistingHistory`, and the
   `synchronizedBuffer` helper those two `Tail` tests alone used. Keep
   every `TestFormat*` test unchanged.
5. **Docs:** remove `tail` from the generated CLI reference via
   `agent-comms-docgen`'s normal regeneration, and add a `CHANGELOG.md`
   entry for this release. Past RFCs (0027, 0008, 0009) that introduced
   and documented `tail` are left as the historical record they are --
   exactly how RFC 0028 left RFC 0007/0027's own text alone when it
   removed `session`.

## Alternatives considered

- **Port `tail` onto the broker model instead of removing it.** Rejected
  by the project owner: `attach` already covers watching a live session
  end-to-end for both providers; a second, Claude-only, file-based path
  to the same information is exactly the duplication RFC 0027 already
  tried to reduce elsewhere in this command surface.
- **Deprecate with a warning first, remove later.** Rejected: pre-1.0,
  this project's own convention (see RFC 0027's own "clean break, no
  deprecation aliases" note) is to make a clean break rather than carry
  a deprecated path across releases.
- **Delete the whole `claudetail` package.** Rejected: `Format` is a real,
  still-used dependency of `live attach --provider claude`, confirmed by
  reading `cmd_misc.go`'s `attach` RunE directly, not assumed from the
  package name alone.

## Compatibility and rollout

Breaking: `agent-comms live tail` is removed with no replacement flag or
alias. A caller who was watching a Claude Code session by its raw session
ID (not yet an Agent Comms runtime) has no direct substitute -- they now
need that session registered as a runtime and reachable through
`live serve` + `live attach`, the same path every other provider already
requires. `CHANGELOG.md` gets a **Breaking** entry.

No change to `live serve`, `live attach` for either provider, any of the
nine worker adapters, or provider session continuity
(`claudepath.SessionPath`, `sessionbind`, `claudeserve`/`codexserve`
themselves are all untouched).

## Security and privacy implications

Neutral-to-positive. Removes the one code path in this project that reads
a provider's session storage directly off disk outside that provider's
own broker/API surface, narrowing to the two adapters (`claudeserve`,
`codexserve`) that already document exactly how they observe a live
session.

## Test and rollout plan

- `go test ./internal/claudetail/...`, `./internal/app/...` after removal.
- `go vet ./...`.
- Manual CLI check: `agent-comms live tail --help` no longer exists
  (`live --help` lists only `serve`/`attach`); `live attach --provider
  claude` against a real broker still renders turns via `claudetail.Format`
  exactly as before.
- `agent-comms-docgen --check` clean after regeneration.
- Full `go test ./...`.
- One local commit; not pushed until independently reviewed per the
  project owner's own instruction for this change.

## Unresolved questions

None.
