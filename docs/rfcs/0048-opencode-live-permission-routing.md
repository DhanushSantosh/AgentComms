# RFC 0048: OpenCode live permission-routing boundaries

- Status: Accepted (2026-10-04)
- Owner: Core/security maintainer
- Scope: OpenCode live adapter and its REST/SSE permission client

## Problem and outcome

OpenCode live workers reuse a host-shared server. REST requests carry the
client's `x-opencode-directory`, but SSE subscriptions omit it. A watcher also
answers every permission event it observes without checking its `sessionID`.
An invocation configured to accept edits can therefore approve another
session's edit, or deny/record another session's request. Project/session
routing must identify the invocation whose policy is being applied.

Three deterministic repetitions on `c6ed603` reproduced both boundaries:
the directory-checking SSE endpoint rejected the subscription, and the actual
live adapter approved a foreign-session edit (`once`) before answering its
own session's control request. These are synthetic HTTP tests; no real
permission, provider credential or account configuration was changed.

## Design

1. `Subscribe` uses the client's existing directory-header helper, just like
   REST calls. An empty directory preserves the existing unscoped transport
   convention; the live worker always supplies its working directory.
2. Each permission watcher is constructed with the exact provider session ID
   returned by session creation or confirmed by session lookup. Its binding is
   immutable for that watcher/turn.
3. Only a nonempty matching `sessionID` may reach classification, edit gating,
   governance, reply or denial tracking. Foreign, absent and malformed session
   identities are ignored, leaving their owner to answer them. An empty watcher
   binding is fail-closed and answers nothing.
4. The live adapter passes its resolved session ID before subscribing/prompting.
   Existing category classification, edit modes and governance decisions are
   unchanged for matching requests.

## Compatibility and rollout

No CLI/MCP flag, signed state, schema, authority role or approval format changes.
The internal constructor changes and its callers/tests are updated together.
Active processes use the old binary until workers are restarted. Existing
servers and provider sessions need not be removed or recreated. Events lacking
session identity no longer receive a speculative reply and may time out; that
is preferable to applying another invocation's policy.

## Alternatives

- A watcher for all server events retains the reproduced cross-session effect.
- Rejecting foreign requests still changes another session's authorization.
- One server per runtime avoids some sharing but changes lifecycle, ports and
  attachment behavior, and does not repair missing directory routing.

## Security and privacy

This binds existing permission decisions to their intended session; it is not
server authentication, an OS sandbox or multi-tenant authority admission.
Provider-default permission behavior remains a separate verification surface:
filtering events cannot govern a provider operation that emits no request.
No credential extraction, provider-config edits or shared-server shutdown is
part of this change.

## Verification and completion

- Reproduce both failing seams before changing production code.
- Confirm directory-preserving SSE requests and unscoped empty-directory control.
- With two sessions and conflicting edit policies on one stream, each watcher
  replies only to its own requests. Foreign events cause neither governance
  calls nor denial records. Missing/malformed identities remain untouched.
- Exercise the live adapter against an owned HTTP/SSE fixture: foreign request
  untouched, own request answered, prompt/result completes, cancellation closes
  the stream. Cover created and resumed sessions.
- Preserve matching-session read/edit/governed decisions; run affected race,
  vet and native candidate CI checks.
- Run an owned real-provider directory/session and permission-mode check before
  claiming the live adapter's end-to-end policy coverage complete. Any separate
  provider-policy failure remains a finding, not an excuse to loosen governance.

## Acceptance

The maintainer accepted this design on 2026-10-04. No policy choice
between approving or rejecting foreign requests is left open: neither is
authorized by this invocation.
