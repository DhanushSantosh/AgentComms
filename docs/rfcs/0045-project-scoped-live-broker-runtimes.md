# RFC 0045: Project-scoped live broker runtime keys

## Status and owners

Accepted, 2026-10-04, by the maintainer's “Approved RFC”. Author: codex-main.
Scope: Agent Comms-managed Codex and
Claude live worker/client/viewer routing, not provider permissions or authority
authentication. OpenCode's native session-ID routing remains unchanged unless
its independent validation reveals the same collision.

## Problem and desired outcome

Runtime IDs belong to a project, but the fixed-port Codex broker indexes its
processes by bare runtime ID. All projects on a host discover the same broker.
A direct real-HTTP regression on b89250e registered `reviewer-runtime` in one
temporary working directory, then registered that same ID for another. The
second registration failed with HTTP 409, "runtime is already registered with
different process configuration." Each provider was a bounded test process;
no real provider credentials were used. An independent real-HTTP Claude
regression reproduced the same HTTP 409 before project-aware clients were
used. Each provider requires its own continuing validation.

The conflict check protects the existing process and must remain. The missing
boundary is between independent projects. Normal use should permit the same
project-local runtime ID in two projects without replacing either process or
mixing their prompts/events. This is a reproduced availability/UX fault, not
a demonstrated cross-project data leak or new authentication boundary.

## Proposed design

1. Derive an opaque broker transport key from the stored project ID and
   logical runtime ID using one shared, domain-separated SHA-256 function
   with an unambiguous length-prefixed encoding of the two IDs.
   Keep the logical runtime ID in signed records, cache filenames, command
   output and user-facing instructions. Use the key only at broker routing
   boundaries. A digest fits existing broker ID validation without a new
   arbitrary-length path or provider argument.
2. Add project-aware clients for Codex and Claude. All register, prompt and
   event-subscription calls use the same mapping. A scoped client never
   silently falls back to an unscoped runtime. Keep existing unscoped client
   APIs and raw HTTP routing for legacy/manual clients; their IDs retain
   their explicitly host-wide meaning.
3. Managed live workers obtain the project ID from their validated project
   configuration, not from display names or working-directory strings.
   Preserve current thread/session caches and configuration-conflict checks
   within one scoped key. A missing project identity fails before launch.
4. `live attach` resolves the project identity from the selected project and
   supports an explicit `--project-id` for viewers without a local checkout.
   Generated worker instructions include enough context to select that exact
   project. An explicitly scoped viewer gets a clear error when its runtime
   is absent; it does not attach to an unrelated legacy runtime.
5. Add explicit `live attach --unscoped` for legacy/manual registrations,
   mutually exclusive with `--project-id`. It uses the existing literal-ID
   client even inside a project checkout. Outside a project, attachment with
   neither scope option retains the old unscoped behavior. Project-aware
   workers and viewers otherwise use scoped routing consistently.

## Alternatives considered

- Requiring host-global logical IDs contradicts their project-scoped storage
  and exposes an undocumented constraint in ordinary multi-project workflows.
- Dropping the configuration conflict or replacing an existing process would
  disrupt the first project and risk routing another project's prompt there.
- Hashing working directories makes identity depend on relocation and aliases.
- Separate random ports per project reintroduce the duplicate-broker/cache-loss
  problem that motivated fixed-port discovery.

## Compatibility and rollout

No signed event, durable schema, approval or logical runtime-ID migration.
Existing broker binaries already accept digest-shaped transport IDs, so this
does not depend on replacing the raw HTTP protocol. Old unscoped registrations
are not silently adopted by new scoped workers. Operators stop their old live
workers and recycle their owned brokers during upgrade before starting scoped
workers; otherwise an old provider may remain running under its legacy key.
Existing per-project provider session caches remain available for continuity.
Call out the routing change and viewer context in Unreleased/operator docs.

## Security and privacy

Opaque keys separate namespaces; they are not secrets or authentication. The
broker retains its documented local-host trust assumptions. Never log or copy
credentials, alter provider config, loosen sandbox validation, or infer a
governance approval from this routing key. Same-project conflicting process
configuration must still fail rather than replace a live process.

## Verification and completion criteria

- Prove independent projects with the same logical runtime ID can register
  and prompt concurrently, with distinct child processes and separate events.
- Exercise Codex and Claude separately with the real broker HTTP path.
- Same-project conflicts still return 409; malformed requests still fail.
- Worker, client and viewer derive the same key; different project/runtime
  pairs differ; project identity remains stable across root relocation.
- Cover scoped absence without fallback, explicit viewer project IDs,
  project-local lookup and legacy/manual unscoped compatibility.
- Verify cached native session continuity, process-crash recovery and observer
  retention, plus bounded output/cancellation and child cleanup.
- Run uncached focused race tests, CLI/reference generation, vet and exact
  native candidate CI; keep existing security and release gates unchanged.

## Unresolved questions

Acceptance is needed for the scoped routing and viewer compatibility design.
Cross-user loopback authentication and provider-backed isolation are separate
security designs, not implied by the namespace fix.
