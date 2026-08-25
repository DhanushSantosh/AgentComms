# CLI output inventory

Companion to RFC 0022. This records the human presentation shape assigned to
the current `internal/app` emit sites before migration. Machine-mode payloads
remain the existing values and envelopes.

Current presentation plumbing:

- 74 direct `c.emit(...)` calls;
- 2 `c.emitWithDelivery(...)` command calls;
- 3 `c.emitTable(...)` command calls;
- every generic human path ultimately falls back to `json.MarshalIndent`.

The additional internal calls between `emit`, `emitTable`, and
`emitWithDelivery` bring the raw textual search total to 80; they are not
separate command surfaces.

## Assigned presentation shapes

| Command family | Commands/results | Default human shape | Details/secondary shape |
|---|---|---|---|
| Bootstrap | `init` | Success summary: project, owner, mode, runtime | Paths, endpoint, actor resolution |
| Version | `version` | Compact key/value block | Build ID, schema/project format, Go/platform |
| Health | `status`, `doctor`, `verify` | Grouped status summary with severity and remedy | Integrity/freshness checks and diagnostic evidence |
| Control room | `control overview`, `control attention`, `control settings` | Sectioned operational summary | Nested counts/settings |
| History/search | `history`, `search` | Timeline/result list | Event hashes, payload metadata, filters |
| Project lifecycle | `project upgrade status/plan`, `project upgrade`, `project delete` | Plan/outcome summary | Per-project actions, backup/restart/verification facts |
| Identity mutation | `agent register/activate/switch-role/rotate-key/elevate-key/rename/suspend/revoke/delete` | Outcome + principal identity/status + next action | Receipt and key fingerprint metadata |
| Identity list | `agent list` | Responsive table | Capabilities/scopes and key details |
| Profiles/config/theme | `profile current/list/use`, `config`, `theme set` | Selection/precedence summary | Full source-specific configuration |
| Task mutation | `task create/offer/claim/start/renew/block/review/complete/cancel/handoff/takeover/lock` | Outcome + owner/status/lease/resources + next action | Offers, risk, handoff, receipt metadata |
| Task list | `task list` | Responsive table grouped/filtered by status | Resources, offers, timestamps |
| Messages | `message post`, `message inbox`, recipient transitions | Post confirmation or obligation-focused inbox table | Per-recipient lifecycle and body/detail view |
| Decisions | `decision create/ack/supersede` | Outcome + title/status/audience | Statement and supersession metadata |
| Approvals | `approval request/approve/reject`, `approval list` | Outcome or pending-approval table | Tier, affected actors, reason, action target |
| Artifacts | `artifact add/show/verify` | Verification/outcome summary | Hash, media type, size, storage metadata |
| Documents | `document create/update/supersede/show/list` | Outcome, detail view, or responsive table | Body, tags, version lineage |
| Environment | `env set/get/delete/list` | Outcome, key/value detail, or table | Updated-by/time metadata; never reveal unauthorized values |
| Drafts/archive | `draft save/list`, `archive` | Local/non-authoritative outcome or list | Draft metadata and archive counts |
| Runtime mutation | `runtime register/configure/heartbeat/status/configure/drain/revoke` | Outcome + connector/health/status | Host, endpoint, scopes/capabilities, presence evidence |
| Runtime list | `runtime list` | Responsive table | Session/presence and active invocation detail |
| Runtime session | `bind-session`, `session`, `interactive-session`, `verify-adapter`, interactive launch | Binding/verification summary | Adapter assumptions, command/path and local session evidence |
| Invocation request/delivery | `invocation request`, `redeliver` | Request outcome plus evidenced delivery stages | Delivery attempt/transport/runtime detail |
| Invocation list/detail | `invocation list`, `inspect`, `next` | Responsive table, grouped detail, or found/empty state | Delivery history, scopes, result linkage |
| Invocation lifecycle | `listen`, `claim/start/wait/resume/complete/reject/expire/cancel` | Event/outcome line; `listen` becomes stream | Runtime, deadline, reason/result metadata |
| Invocation policy | `policy set/show` | Policy summary | Trusted actors, modes, scopes, sensitive-action rules |
| Sessions | session lifecycle commands and heartbeat | Outcome line or compact summary | Durable versus ephemeral status and timestamps |
| Updates | `update check/apply` | Availability or completion summary | Channel, versions, verification/download facts |
| Instructions | `agent-instructions` | Path/context header plus instruction content | Actor-resolution metadata |

## Pass-through and streaming surfaces

These do not use generic decorative output:

| Surface | Contract |
|---|---|
| `mcp` | Stdio MCP protocol; stdout must remain protocol-only |
| `completion` | Native shell completion source |
| `export jsonl` | Existing export bytes; not wrapped in presentation |
| `export markdown` | Existing document export |
| `tui` | Full-screen Bubble Tea application |
| `daemon serve` | Hidden service process/log contract |
| `claude/codex serve` | Broker process/log contract |
| `claude/codex attach` | Live provider event stream |
| `runtime worker` | Long-running worker status and child process boundary |
| `runtime interactive-serve` | PTY/pass-through lifecycle |
| `watch` | Candidate for semantic human/plain/JSONL event projection |

## Migration acceptance per command

A command is migrated only when:

1. its human output no longer exposes generic indented JSON;
2. redirected output contains no ANSI or animation;
3. warnings/progress stay on stderr;
4. `--json` produces the pre-existing envelope and payload;
5. empty, success, warning/degraded, and error states are covered where the
   command can produce them;
6. user-controlled values cannot inject terminal control sequences.

