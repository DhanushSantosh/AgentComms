# RFC 0025: Bound approvals, verified first install, and authority capacity isolation

## Status

**Accepted, 2026-09-02.** The project owner approved this design before
implementation, per `docs/rfcs/README.md` and `docs/development-workflow.md`.

## Problem and desired outcome

The security review identified three medium-severity defects:

1. approval records authorize only an action string and discard their requested
   expiry, so a later contract or invocation can differ from what was reviewed;
2. first-install scripts execute a verifier authenticated only by a checksum
   obtained from the same mutable release assets; and
3. long-lived authority SSE streams occupy the same bounded request capacity as
   health checks and mutations.

The system must bind an approval to the operation a reviewer saw, establish an
independent first-install verifier trust root without requiring end users to
install Cosign, and retain capacity for control-plane operations under stream
load.

## Proposed design

### Bound approvals

Approvals for contract publication and approval-gated invocations carry a
canonical SHA-256 subject digest and an expiry in durable state. The transition
validator reconstructs the digest from the normalized operation and requires an
unexpired approved record with the matching action and digest. Existing
action-only approvals cannot authorize these operations and must be renewed.

### First install

Release installers are version-pinned and contain the expected SHA-256 digest
of the release verifier. They verify that digest before executing the verifier;
the verifier continues to verify the requested CLI binary with its Sigstore
bundle. The installer therefore trusts its tag-pinned source, not mutable
release checksums. Mutable latest-channel installs are not presented as an
independently verified bootstrap; after the first verified install, the
already-trusted in-product updater remains the convenient latest-install path.

### Authority capacity

SSE streams use a dedicated bounded connection pool. Normal HTTP admission
continues to protect short requests, so stream holders cannot exhaust mutation
or health capacity. Rejected streams receive the existing retryable-unavailable
response shape.

## Alternatives considered

- Require an independently installed `cosign`: secure but imposes unnecessary
  end-user setup.
- Continue trusting a verifier checksum from release assets: rejected because
  it is circular trust.
- Leave streams under global admission: rejected because idle streams can deny
  unrelated control-plane requests.
- Grandfather action-only approvals: rejected because it leaves the vulnerable
  authorization path reachable.

## Compatibility and rollout

New approval fields are additive in persisted state, but action-only approvals
for contracts and approval-gated invocations must be renewed. Install commands
must specify a released version when using the standalone bootstrap scripts.
The existing direct in-product updater is unchanged.

## Security and privacy implications

The design prevents approval substitution and expiry bypass, makes verifier
replacement detectable without a user-installed verifier, and limits stream
denial of service. No additional user data is collected.

## Test and rollout plan

- Regression tests cover mismatched and expired approval subjects plus valid
  approved operations.
- Installer tests prove a verifier whose digest differs from the pinned value
  is never executed.
- Authority tests keep health and mutation capacity available while the stream
  pool is full.
- Run focused package tests, `go vet`, and the repository's normal Go suite
  where the environment permits it.

## Unresolved questions

Per-identity authority authentication and durable project/principal quotas are
follow-up hardening work: the present HTTP service delegates identity
authentication to its deployment boundary, so adding a new application-level
identity contract is intentionally outside this RFC.
