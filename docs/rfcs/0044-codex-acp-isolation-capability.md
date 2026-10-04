# RFC 0044: Explicit Codex ACP isolation capability

## Status and owners

Proposed, 2026-10-04. Owner: the project maintainer; author: codex-main.
Implementation requires acceptance. This extends RFC 0042's fail-closed
capability rule to the separately registered `codex-acp` adapter.

## Problem and desired outcome

The runtime CLI passes `--codex-ignore-user-config` to every worker adapter.
`codexACPAdapter.Validate` accepts it, but `Execute` neither forwards nor
implements it: it launches the ACP wrapper with `INITIAL_AGENT_MODE` only.
A direct regression on c50385a expecting rejection failed because validation
returned nil with `CodexIgnoreUserConfig=true` and `Sandbox=read-only`.

This proves silent acceptance, not an exploit or that every provider loads
unsafe configuration. A caller requesting configuration isolation should
either receive that guarantee or fail before launching a provider.

## Proposed design

Reject `CodexIgnoreUserConfig=true` in `codexACPAdapter.Validate` before a
provider process or runtime execution starts. Name the unsupported adapter
in the diagnostic and point to `--adapter codex`, whose native exec path
passes the actual `--ignore-user-config` flag. Keep ordinary ACP mode,
sandbox mapping, prompt behavior and session continuity unchanged.

Clarify public flag help and worker documentation: the isolation option is
supported by Codex exec, not by the current Codex live or ACP adapter. Keep
the flag name and configuration field stable. Native ACP isolation support
would require a separate provider capability review and behavioral proof.

## Alternatives considered

- Continuing silent acceptance provides a false isolation assurance.
- Automatically substituting exec changes session and transport semantics.
- Passing an undocumented wrapper option does not establish isolation.
- Copying credentials or changing the user's provider configuration creates
  unrelated storage and continuity risks.

## Compatibility and rollout

Only configurations requesting the previously ineffective option fail.
Default ACP runs remain supported. Record the intentional tightening in
Unreleased notes and regenerate the CLI reference after changing flag help.
No event, schema, approval, key, database or provider credential changes.

## Security and privacy

The boundary is truthful capability reporting; it does not claim normal ACP
runs isolate user configuration. No permission bypass, credential copies or
user-config mutation is introduced.

## Test and rollout plan

1. Preserve the direct red regression and make it pass through rejection.
2. Exercise `worker.New` with a real test service to prove rejection happens
   before execution; verify the default ACP configuration remains accepted.
3. Retain native exec argument tests proving the genuine isolation flag is
   forwarded, plus ACP sandbox/model validation and session tests.
4. Run uncached worker/ACP race tests, vet, generated-reference checks and
   exact candidate CI without weakening existing gates.

## Unresolved questions

Acceptance authorizes rejection only. Provider-backed ACP isolation and
other adapter option capabilities remain separate audit work.
