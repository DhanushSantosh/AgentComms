# RFC 0049: Enforce managed OpenCode live permission policy

- Status: Accepted (2026-10-04)
- Owner: Core/security maintainer
- Scope: OpenCode live server ownership, turn preparation and native permissions

## Problem and desired outcome

RFC 0048 routes permission requests to their exact session. It does not make
OpenCode emit those requests. The owned native OpenCode 1.18.33 probe created a
synthetic file with a deny-edit watcher, with zero observed or pending permission
requests. Setting only that disposable session's policy to ask made the same
watcher deny the edit. This is a reproduced enforcement gap, not a hypothetical
provider configuration concern.

Ask-only is insufficient: an `always` edit approval in another session in the
same project instance let the owned session edit without a new request despite
its ask rules. Resetting only the disposable server's owned project instance
cleared that approval and restored denial while retaining the session history.
The controls are explicitly diagnostic; the unsafe control passing is evidence
of the defect, not release approval.

The supported outcome is enforced read/edit/governance behavior across created
and resumed sessions, repeated invocations and permission-mode changes, without
weakening native deny restrictions or resetting anyone else's server/session.

## Provider evidence

The installed server's `/doc` exposes `permission` rulesets on session creation
and PATCH `/session/{sessionID}`, not on the prompt body. Rules have permission,
pattern and action fields. Its instance disposal endpoint was exercised only
against the probe's own ephemeral server and disposable project.

Pinned upstream [permission implementation](https://raw.githubusercontent.com/anomalyco/opencode/v1.18.33/packages/opencode/src/permission/index.ts)
evaluates saved approvals after the request ruleset; these saved approvals live
in project-instance state. The [prompt implementation](https://raw.githubusercontent.com/anomalyco/opencode/v1.18.33/packages/opencode/src/session/prompt.ts)
combines agent rules before session rules. These facts explain why a session
ask override and a shared persistent server are not a complete fix.

## Proposed design

### Runtime-owned server module

Give each managed OpenCode live worker its own server process on an assigned
loopback port. Ownership is a retained process handle, not a healthy URL, PID
alone or legacy cache file. Never adopt or dispose the host-shared port 4096
server. Create adapter state per worker rather than adding mutable state to the
global adapter registry. Keep process lifecycle and turn preparation behind a
small internal module interface; other adapters remain unchanged.

Keep the owned server alive across invocations while the worker runs, retaining
its endpoint and live attach capability. On worker shutdown, cancellation or
startup failure, close/reap only its owned process tree using the accepted
platform ownership mechanisms. Provider conversation history remains persisted;
worker restart may choose a new endpoint and reports the new attach command.
One-shot workers stop their owned server when they exit. This replaces the old
unowned server surviving worker exit and must be documented as a rollout change.

### Prepare each invocation before prompting

1. Serialize turn preparation within this worker. Close its previous stream,
   dispose only its owned project instance, and require successful disposal
   before continuing. This clears prior native saved approvals. There is no
   shared-server fallback if ownership or reset cannot be established.
   Native HTTP acknowledgment is not teardown proof. Subscribe to the owned
   server's global event stream, establish readiness, and require the exact
   project's disposal-completion event before preparation. Missing completion
   closes the owned server and prevents prompting. The native initial connected
   frame precedes listener registration; wait for its heartbeat readiness rather
   than racing the reset against stream startup.
2. Resolve/create/resume the managed session and require matching returned
   session identity and project directory. Preserve the conversation; reset is
not session deletion. Resolve the effective native agent and original native
   session restrictions explicitly, rather than guessing a default agent.
3. Construct an ordered restrictive ruleset: prepend a catch-all ask, retain
   effective agent/session rule order and native denies, and convert native
   allows to asks. Leave denies and asks unchanged. Use native wildcard
   evaluation, not a second home-grown evaluator. The watcher remains the
   existing read auto-approval/edit-mode/governance decision maker.
4. Distinguish original native session restrictions from the module's previously
   installed managed rules so repeated preparation does not accumulate stale
   copies. Bind any local bookkeeping to the exact runtime/project/session and
   validate the current rules before treating them as a managed copy. If original
   restrictions or native agent selection cannot be established, fail closed.
   Operator changes to native rules must not be discarded or loosened.
5. Apply the rules to the resolved session, read them back and require exact
   agreement before opening its scoped stream and prompting. Unknown actions,
   unsupported capability, missing identity, request failure or disagreement
   fails the invocation with an actionable reason. No silent default permission
   fallback and no configuration or credential edits.

Human activity on the same owned native endpoint remains an explicit local
operator capability, not an OS sandbox guarantee. Concurrent interactive prompts
or external policy changes during a managed turn are not a supported way to run
a second agent; separate runtimes receive separate owned instances. The rollout
must explain this constraint rather than imply the watcher is server access
control.

### Accepted native PATCH compatibility step

The maintainer accepted this bounded amendment on 2026-10-04. Native PATCH
appends permission rules instead of replacing them. Before installing managed
rules, create one preparation message with `noReply: true`, no content parts,
and `tools: {"*": false}`. This disables tools without executing a model turn,
replaces the session permission baseline with a wildcard deny, and returns the
actual provider-selected agent identity. Resolve that exact identity; never
guess the default from a sorted agent list or fetch resolved configuration.

Preserve original restrictions before preparation. Append the restrictive rules,
require exact full readback including the initial wildcard deny, then delete only
the exact synthetic message created by this operation. Existing conversation
messages must remain untouched. Preparation identity, policy verification and
cleanup failures all prevent the actual prompt. Retain the exact owned marker
identity for bounded recovery; never search for or delete arbitrary empty
messages. Native history preservation and response-loss recovery require tests
before closure.

Native preparation also invokes the provider's revert cleanup before checking
`noReply`. Reject any non-null native session Undo/revert state before creating
the synthetic message, preserving its undoable history. The operator must
restore or resolve that state through the native client; preparation must not
commit a history change merely to establish policy.

## Alternatives

- A session catch-all ask alone was disproved by the inherited-approval control.
- Disposing a shared project instance interrupts other agents and changes their
  approvals; this proposal confines disposal to an owned runtime server.
- An environment override on a new shared server neither repairs existing
  shared servers nor provides per-runtime saved-approval isolation.
- Native blanket allow/deny mapping skips watcher governance and can loosen
  configured denies. Disabling all tools removes legitimate functionality.
- ACP is an available interim choice, not a substitute for completing native
  live policy enforcement and attach compatibility.

## Compatibility, security and rollout

No signed authority schema, roles, CLI mode vocabulary or credential format
changes. Existing native servers are left running and untouched. Restart managed
workers to use the owned-server behavior; old workers retain old behavior.
Managed session IDs/history may be resumed only after ownership, identity and
native restrictions are verified. Existing public UUID-only explicit OpenCode
session validation is a separate compatibility issue, not silently fixed here.

Keep provider permission APIs/version differences fail-closed. Record synthetic
evidence only; never log credentials, resolved provider configuration or private
prompt contents. This design does not prevent a trusted local operator from
directly changing their own provider or host, or establish mutually untrusted
tenant isolation.

## Required verification before closure

- The original actual native deny-edit repro goes green without a test-only
  ask override. Actual adapter/worker call-site tests prove enforcement, not
  merely a client helper configured differently from production.
- Native created/resumed and two-turn controls cover read success, acceptEdits
  success, plan/manual/auto/dontAsk edit denial and governed shell/external
  access denial. Native explicit deny rules remain denied even in acceptEdits.
- A foreign runtime's always approval cannot suppress another runtime's request;
  a prior turn's always approval cannot bypass a later restrictive mode.
- Preparation fails before prompt on reset/apply/readback/agent/identity errors;
  test unknown native actions and modified session rules as well as success.
- Owned instance reset retains session history and the attach endpoint. Verify
  an actual attached client reconnects and observes the next turn. Do not claim
  attach compatibility from HTTP responses alone.
- Worker lifecycle covers two runtimes, same IDs in different projects,
  startup failure, restart, cancellation, one-shot exit and unrelated provider
  survival. Run real Linux evidence plus native Windows/macOS ownership tests.
- Run affected race/vet, whole-tree gates and exact candidate CI. Update product
  docs and the audit ledger. Diagnostic controls cannot replace these gates.

## Acceptance and implementation gate

The maintainer accepted the owned-runtime server lifecycle and restrictive
per-turn native policy design on 2026-10-04. If real native attach/reset or deny-rule
preservation cannot satisfy the criteria, return to design review; do not quietly
ship an ask-only patch or remove required functionality to obtain a green test.

### Accepted native attach compatibility scope

The maintainer accepted the normal full native `opencode attach` TUI as the
supported continuous watcher on 2026-10-04. Actual native terminal verification
retained the same owned endpoint and rendered both turns across instance reset.
The optional OpenCode 1.18.33 `--mini` client rendered the first turn but stopped
watching after reset. Its upstream transport treats matching instance disposal
as failure instead of reconnecting. Document this provider limitation and direct
operators to the normal full TUI; do not weaken/reset less frequently to preserve
mini's stream. The mini diagnostic remains known-red evidence, not a passing
release gate or an AGC feature silently removed. Full native attach across reset
is still required before closure.
