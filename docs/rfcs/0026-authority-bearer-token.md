# RFC 0026: Authority bearer token

## Status

**Accepted, 2026-09-02.** The project owner asked to proceed with the
security-audit follow-up after RFC 0025 left authority application
authentication as separate hardening work.

## Problem and desired outcome

The shared PostgreSQL authority service still accepted HTTP requests from any
caller that could reach it. Signed control-plane commands continued to verify
project identity, roles, leases, approvals, and event integrity, but transport
admission itself was anonymous. That left three avoidable risks:

1. unauthenticated callers could allocate arbitrary project rows through
   `POST /v1/projects`;
2. unauthenticated callers could read project state, events, streams, metrics,
   and verification responses when the service was reachable; and
3. mutation rate limits keyed on caller-supplied actor strings before command
   verification, so anonymous callers could spread pressure across synthetic
   actors.

The desired outcome is a small application-level access control that protects
the authority service before request bodies are trusted, without replacing the
existing signed-command authorization model.

## Proposed design

Add an optional authority bearer token to the HTTP server. When configured,
all authority endpoints except `GET /health/live` and `GET /health/ready`
require:

```text
Authorization: Bearer $AGENT_COMMS_AUTHORITY_TOKEN
```

Health checks stay unauthenticated so process supervisors and load balancers
can keep using them without holding write-capable service credentials.
`/metrics` is protected because it exposes service state and pressure signals.

Production `agent-comms-server` startup requires
`AGENT_COMMS_AUTHORITY_TOKEN`, in addition to the existing TLS and explicit
service signing-key requirements. Development keeps the token optional at the
library level so existing in-process tests and local harnesses remain simple,
but the supplied Compose stack passes the token through and fails fast if it is
missing.

Service-mode clients read the same environment variable at runtime and attach
it to remote authority requests. The token is intentionally not persisted in
`.agent-comms/config.json`; project runtime config is easier to copy,
archive, or commit by accident than process secret configuration.

The bearer-token comparison uses fixed-size SHA-256 digests and constant-time
comparison to avoid leaking prefix or length information through the obvious
HTTP authorization path.

## Alternatives considered

- Continue relying only on a reverse proxy. Rejected because the service had
  no in-app backstop if the proxy was misconfigured or bypassed.
- Add per-principal HTTP credentials immediately. Stronger long term, but it
  needs a token distribution, rotation, and identity mapping design that would
  be larger than this phase-one hardening.
- Persist the authority token in project config. Rejected because it would put
  a service-wide secret into a workspace file.
- Require authentication for health endpoints. Rejected because health checks
  do not expose project data or mutate state, and keeping them public avoids
  pushing service tokens into generic infrastructure probes.

## Compatibility and rollout

Personal mode is unchanged. Existing development code paths that construct an
authority HTTP server without a token keep working. Production service
deployments must set `AGENT_COMMS_AUTHORITY_TOKEN` on the server and on every
service-mode client or daemon process.

An old client without the token will receive the existing structured
authorization error shape with HTTP 401 from protected endpoints. Operators can
roll out by setting the token on clients first, then requiring it on the
server.

## Security and privacy implications

This adds a coarse service-admission secret in front of project creation,
state reads, event streams, verification, metrics, deletion, and signed command
submission. It reduces exposure from accidental network reachability and gives
the in-app service a default production backstop even when a reverse proxy is
misconfigured.

This does not replace project-level signed commands, owner/orchestrator
authorization, elevated-key checks, or approval binding. It is also not a full
per-user identity system; callers holding the service token still share the
same HTTP admission identity. Per-principal quotas and credential rotation can
be designed later on top of this boundary.

## Test and rollout plan

- Unit-test that protected authority endpoints reject missing bearer tokens.
- Unit-test that health endpoints remain public.
- Unit-test that the remote client attaches a trimmed bearer token.
- Unit-test that production server startup rejects a missing authority token.
- Run focused package tests and `go vet` for the touched authority, client,
  init, daemon, service, app, and server packages.
