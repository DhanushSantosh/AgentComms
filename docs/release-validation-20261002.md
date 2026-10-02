# Release validation: recovery, read bounds, and CLI/MCP parity

Date: 2026-10-02. Code under test: `d0d9a1c` (the source tree merged into
`dev` as `260e603`). This pass used only disposable personal projects and a
disposable local Postgres 17 instance. It is validation of these three backlog
areas, not release promotion or a claim about an arbitrary production load.

## Recovery and cache lag

- A Postgres authority and local SQLite cache delivered 61 invocations to two
  registered local-process runtimes. Twenty serial requests alternate targets;
  every third request is committed to authority without first applying it to
  the cache. A subsequent burst queues 40 more requests at authority while
  the cache is behind, then four concurrent callers request synchronization.
  The test proves the lag, checks that all four calls complete, verifies the
  received runtime for serial requests, and checks exactly one authoritative
  delivery to the intended runtime plus a `NOTIFIED` outcome for every queued
  request. It does not measure whether the four callers share one fetch.
- The expanded Postgres test passed three consecutive runs with the race
  detector and three more without it. The coordinator's transient
  failure recovery test passed 100 runs. Service tests for lost-response
  idempotent replay and missing daemon socket recovery passed five runs.
- These tests cover repeated work, a queued burst, concurrent sync calls and
  forced cache lag. They do not measure long-duration throughput with many
  simultaneously active workers. No production capacity claim follows.

Verification on this patch: `go test ./...`, `go vet ./...`, Postgres-backed
`go test -count=3 ./internal/authority/... ./internal/daemon/...`, and
`go test -race ./internal/daemon ./internal/app ./internal/mcp
./internal/localcache` passed. The local race run overlapped the full suite;
`internal/app` finished in 599.9 seconds, just under its ten-minute per-package
timeout. The expanded Postgres burst test passed an additional three
race-enabled repetitions. That timing is a local load observation, not
evidence of a race.
The merged `dev` revision `260e603` also passed its CI and site deployment
workflows before this validation patch.

## Read surfaces and cursor semantics

| Surface | Current behavior | Validation result |
| --- | --- | --- |
| History over CLI, MCP, daemon, and authority | Default 100, maximum 500; opaque sequence cursor | CLI and MCP returned identical pages at limits 1, 3, and 500; one-record cursor walk returned each event once. Shared `PageRequest.BoundedLimit` rejects values above 500. |
| Local drafts | Default 100, maximum 500; ordered by update time | Bounded query, but no continuation cursor. Older drafts cannot be reached through `list` if more than the limit exist. |
| Message inbox | Optional `--limit`, with zero meaning unlimited | Stable order before trimming; no cursor. `--limit` bounds returned rows, not state loading. |
| Task, agent, document, approval, invocation, runtime, environment lists | Full state snapshot, filtered locally where offered | No pagination or finite result limit. Output can grow with project history and entity count. |
| `history --all` and `--grep` | Reads every bounded history page into one result before filtering | Explicit full-log operation; memory use scales with log size. |

The full-state list behavior is a real scope limit, not a demonstrated
failure at supported project sizes. A bounded list API would change public
CLI/MCP behavior and needs design before implementation. Trusted self-hosted
teams remain the accepted release scope; do not advertise these lists as
bounded. Continue to use the bounded history API for large event logs.

## CLI/MCP authoritative parity

An isolated personal authority received equivalent task creation and message
posting through the actual CLI and MCP adapters. The resulting authoritative
records agreed on title, status, repository, branch, resources, message kind,
actor, subject, body, and recipients. Both adapters returned the same history
pages and cursor chain. Generated entity IDs, timestamps, and receipts were
not compared as equal because each command is a separate write.

## Outcome and remaining evidence

No data loss, authorization bypass, or incorrect delivery was observed in
these exercised cases. Follow-up product work is bounded, cursor-based entity
listing, especially drafts beyond the first page. For release confidence, keep
the existing Postgres-backed integration CI job and the platform, race,
security, installer, and site checks green on the exact release candidate.
A full production-scale soak with concurrent workers is still untested here.
