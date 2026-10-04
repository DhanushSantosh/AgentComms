# RFC 0042: Explicit Codex live user-config isolation capability

## Status and owners

Accepted, 2026-10-04. Owner: the project maintainer. Proposed by codex-main
during the final release audit. The owner accepted this design in the current
project chat ("approved RFC"). Acceptance authorizes this fail-closed
compatibility change, not a release-gate waiver.

## Problem and desired outcome

`runtime worker --codex-ignore-user-config` promises isolation from user MCP
and tool configuration. The exec adapter passes the installed provider's
`--ignore-user-config` flag. The live adapter forwards the boolean to the
broker, but the broker never uses it. A caller requesting isolation therefore
gets a process using the normal user configuration without a diagnostic.

Installed Codex 0.149.1 documents the flag for `exec`, not `app-server`.
Its generated thread/start and thread/resume schemas contain config overrides
but no user-config isolation field. Overrides of selected keys are not proof
that the complete user configuration was ignored. The desired outcome is a
truthful, fail-closed capability boundary without copying credentials or
modifying the user's provider configuration.

## Proposed design

1. Reject `CodexIgnoreUserConfig=true` in codex-live worker validation before
   registering or launching a runtime. Explain that the current native
   app-server cannot provide this isolation and point to the existing codex
   exec adapter, which supports the flag.
2. Independently reject `ProcessConfig.IgnoreUserConfig=true` at the broker
   process-config boundary so direct broker clients cannot bypass the check.
   Keep the serialized field; older clients receive an explicit validation
   error instead of silent acceptance. Do not start a subprocess on rejection.
3. Leave ordinary codex-live execution, sandbox selection, additional roots,
   thread continuity and the codex exec adapter unchanged. Do not silently
   substitute exec for a live runtime: that changes continuity semantics.
4. Clarify the public flag help and adapter documentation: isolation is
   supported by codex exec; it is unavailable for current codex-live. A future
   native implementation requires documented provider support, a capability
   check and tests proving that an adversarial user config is not loaded.

## Alternatives considered

- Continue ignoring the option: rejected because it creates a false security
  assurance.
- Pass an invented app-server flag/config key: rejected; absence of an error
  would not establish isolation.
- Override a few MCP/tool keys: rejected as incomplete and provider-version
  dependent.
- Create a temporary CODEX_HOME and copy or symlink auth files: rejected for
  this release because credential-storage modes, OAuth refresh, local keyring
  behavior and history continuity require a separate design and verification.
- Fall back to exec automatically: rejected because it changes live session
  semantics without caller consent.

## Compatibility and rollout

No durable event or database schema changes. Existing live configurations
without the isolation option continue working. Configurations that previously
requested the ineffective option now fail before launch; this intentional
tightening must be called out in Unreleased notes. Keep flag and JSON field
names stable. Operators can explicitly choose the codex exec adapter when
they require this isolation.

## Security and privacy

The rejection prevents false claims of config isolation. It does not claim
that ordinary live runs are isolated from user configuration, and does not
expand filesystem permissions. No credential copies, provider config edits,
model override, sandbox bypass or new network authentication mechanism.

## Verification and rollout plan

- Regression proving worker validation rejects the requested option before
  any broker call while the default remains accepted.
- Direct broker registration rejection, with a launch sentinel proving no
  subprocess was started.
- Keep exec argument coverage showing the genuine isolation flag is retained.
- Re-run live process continuity, start/resume sandbox and extra-root tests,
  worker tests, vet, generated CLI reference and native platform CI.
- Validate documentation describes the limitation rather than claiming the
  isolation was implemented.

## Unresolved questions

Supporting native app-server config isolation later is a separate capability
decision, not inferred from this proposal. This RFC implements explicit
rejection of the unsupported option, not native isolation.
