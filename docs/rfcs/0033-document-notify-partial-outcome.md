# RFC 0033: Structured `document create --notify` outcomes and a notify retry

## Status

**Accepted, 2026-09-05.** The project owner accepted this while reviewing
[UX-07 of the release UX audit](research/2026-09-05-release-ux-audit.md)
(`docs/research/2026-09-05-release-ux-audit.md`), per `docs/rfcs/README.md`.

Changes `document create`'s machine-visible JSON response and adds a new
public command, so it requires review.

## Problem and desired outcome

`document create --notify <principal>` posts a DECISION message to each
named principal after the document itself commits. A failed notification
(the audit's reproduction: a nonexistent recipient) is reported only as a
raw line on stderr, and that line is itself suppressed under `--quiet`.
The JSON envelope always returns `ok:true` with the document-creation
event and nothing else -- there is no field a script can check to learn
that a requested notification failed. The document is genuinely created
either way (this is not a claim that document creation itself fails); the
gap is that a caller relying on `--json`/`--quiet` output has no way to
tell "fully succeeded" from "document created, notification silently
dropped."

Desired outcome: the response says, in the JSON envelope itself, whether
every requested notification succeeded, and a caller can retry only the
failed ones without duplicating the document or any notification that
already went out.

## Proposed design

### 1. Structured per-recipient result

`document.create`'s emitted result gains a `notify` field (omitted
entirely when `--notify` wasn't used, so the common no-notify case's
response shape is unchanged):

```json
"notify": [
  {"principal": "reviewer", "message_id": "msg-5:doc-1:reviewer", "status": "sent"},
  {"principal": "nonexistent-agent", "message_id": "msg-5:doc-1:nonexistent-agent", "status": "failed", "error": "active message recipient nonexistent-agent is required"}
]
```

This is visible via `--json` unconditionally -- `--quiet` suppresses
supplementary human-oriented text, never core JSON data, and this fixes
exactly the case where the *only* representation of a real outcome was
supplementary text. The existing stderr warning line is kept for
interactive human use; it is not the fix, the JSON field is.

### 2. Retry without duplicating anything

The notify message ID is deterministic:
`fmt.Sprintf("msg-%d:%s:%s", len(documentID), documentID, principal)`. A
failed notification's message was therefore never created; a succeeded
one already exists under that exact ID. `document notify --id
<documentID> --notify <principal>...` re-runs only the notification step
for an existing document, one recipient at a time, using that same
deterministic ID -- a principal whose message already exists gets a
`"status": "already-sent"` result without a second message.post attempt,
not a duplicate-ID error surfaced as a fresh failure. The document
itself is never re-created or modified; this command requires the
document to already exist and only touches the notification messages.

**Amendment, 2026-09-05 (pre-release, caught by an independent review
before this shipped):** the ID scheme as originally accepted here was
`"msg-" + documentID + "-" + principal`, plain dash-joining with no
length prefix. That is ambiguous whenever either ID itself contains a
dash: document `a-b` notifying `c` and document `a` notifying `b-c`
both produce `msg-a-b-c`, so creating the second collided with the
first's already-sent message and silently reported `"already-sent"`
without `b-c` ever actually being notified -- confirmed with a live CLI
reproduction. The length-prefixed scheme above (`msg-%d:%s:%s`) fixes
this: the decimal length is immediately followed by `:`, a character no
decimal digit ever produces, so the split point between documentID and
principal can never be misread regardless of what either one contains.
No other part of this RFC's design changes.

**Amendment 2, 2026-09-05 (same review, round 2 -- two more gaps the
scheme change itself introduced or left open):**

- **Retry no longer idempotent across the ID-scheme change.** A
  document notified under the pre-amendment-1 scheme, then retried
  after this fix, recomputed the new-scheme ID, found nothing there,
  and sent a genuine second notification -- reproduced live.
  `notifyDocumentRecipient` now checks both the current and the legacy
  ID for an existing valid notification before deciding to send.
- **ID presence alone is not proof of an actual notification.** Message
  IDs are not a namespace this tool exclusively owns; an unrelated
  message occupying a notify message's exact expected ID was treated as
  `"already-sent"` on ID presence alone, silently skipping the real
  notification. A candidate message under either ID is now also checked
  against its actual Kind (`DECISION`), Body (the exact acknowledgement
  text, which embeds documentID), and recipient list before being
  trusted as a genuine prior notification.

No other part of this RFC's design changes.

### 3. What does not change

`document create`'s exit code is unaffected: a notification failure was
never a document-creation failure and still isn't -- the document commits
regardless, exactly as today. `--notify`'s existing early-recipient-
validation-when-possible behavior is unchanged. No new event type: this
is still `message.post` under the hood, exactly as `--notify` already
uses today.

## Alternatives considered

- **Fail the whole `document create` if any `--notify` fails.** Rejected:
  the document is a real, independently useful artifact; discarding an
  already-committed document because an unrelated notification failed
  would be a worse outcome than what exists today, and the audit's own
  guidance is explicit that an already-committed document must never look
  like "nothing happened."
- **Emit a synthetic `document.notify.failed` event into the signed log.**
  Rejected: a failed notification attempt is not project state worth
  making durable and auditable forever; the JSON response is the right
  place for this, not the event log.
- **No retry command; tell the caller to re-run the whole
  `document create --notify`.** Rejected: that would either duplicate the
  document (if given a new ID) or hit "document already exists" (if given
  the same one), neither of which actually retries just the failed
  notification the caller wanted.

## Compatibility and rollout

Additive. `notify` is a new, optional response field, present only when
`--notify` was used; every existing `document create` call without
`--notify` is byte-for-byte unaffected. `document notify` is a new
command with no existing behavior to preserve. `CHANGELOG.md` gets an
**Added**/**Fixed** entry.

## Security and privacy implications

None beyond what `--notify` already does today: it posts the same kind of
message (`DECISION`) to the same kind of recipient, through the same
`message.post` transition and its existing authorization path.

## Test and rollout plan

- All-success, partial-success, and all-failed `--notify` fixtures assert
  the `notify` field's per-recipient status and that `--quiet` does not
  remove it from the JSON envelope.
- `document notify` retry test: a document with one sent and one failed
  notification; retrying re-attempts only the failed one and reports
  `"already-sent"` for the one that already went out, with no duplicate
  message.
- `go build`, `go vet`, `go test ./...`; regenerated docs reference;
  `docs:check`.

## Unresolved questions

None.
