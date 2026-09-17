# RFC 0032: `message show`

## Status

**Accepted, 2026-09-05.** The project owner accepted this while reviewing
[UX-04 of the release UX audit](research/2026-09-05-release-ux-audit.md)
(`docs/research/2026-09-05-release-ux-audit.md`), per `docs/rfcs/README.md`.

Adds a new public command, so it requires review.

## Problem and desired outcome

`message inbox` lists messages; there is no way to read one message's
subject and body directly. RFC 0027 added a uniform `show` to every other
domain that previously only had `list` (`task show`, `agent show`,
`approval show`, `decision show`) but did not cover messages -- an
oversight, not a deliberate exclusion; nothing in RFC 0027 argues messages
should be different. UX-04 (`docs/research/2026-09-05-release-ux-audit.md`)
reproduced the resulting gap: a user has to already know to pass
`--details` or `--json` to `inbox` to read a message's body at all.

Desired outcome: `message show --id <id>` exists, built on the same
`entityShow` factory every other domain's `show` already uses.

## Proposed design

`c.entityShow("message", lookup)` (`internal/app/cmd_core.go`, already
shared by `task`/`agent`/`approval`/`decision`), registered as a new
`message show` subcommand alongside the existing `post`/`inbox`/`ack`/
`reject`/`complete`/`resolve`. The lookup returns `Subject`, `Body`,
`Kind`, `From`, `Status`, and the per-recipient obligation list --
everything `inbox --details` already exposes, just addressable by ID
directly rather than requiring the caller to already be looking at the
list.

No new state, no new event type, no change to `message post`'s existing
kinds or obligation lifecycle. Purely a read path, exactly like the other
three `show` commands RFC 0027 already shipped.

## Alternatives considered

- **Fold into `inbox --details` only, no dedicated command.** Rejected:
  that still requires listing first and already exists; the actual gap is
  addressing one message directly, matching every other domain's `show`.
- **A different verb** (`message read`, `message view`). Rejected for
  consistency: every other domain uses `show`; a different verb here would
  be the inconsistency RFC 0027 was written to remove.

## Compatibility and rollout

Purely additive. No existing command's output or behavior changes.
`CHANGELOG.md` gets an **Added** entry.

## Security and privacy implications

None. Reads state a message's own recipient can already see in full via
`inbox --details` or `--json`; `entityShow` has no separate authorization
path from any other read command.

## Test and rollout plan

- CLI test: `message show --id <id>` returns subject/body/recipients for
  an existing message; a missing ID returns the same `not found` shape
  `entityShow`'s other callers already produce.
- Regenerated docs reference (`sites/docs/src/generated/reference.json`);
  `docs:generate:check` and `docs:check` stay green.
- `go build`, `go vet`, `go test ./...`.

## Unresolved questions

None.
