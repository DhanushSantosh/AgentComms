# Team service deployment

> The structured product guide is [Deploy the team service](site/operations/deploy.md). This file remains a compatibility target for existing links.

This deployment is needed when users or agents coordinate across multiple
machines. Local-only projects should use the default personal mode and do not
need this stack.

The authority is a stateless Go service backed by PostgreSQL. PostgreSQL is
the source of truth.

## Development deployment

Set the database password and service signing key, then start the supplied
Compose stack:

```sh
export POSTGRES_PASSWORD="$(openssl rand -hex 24)"
export AGENT_COMMS_SERVICE_PRIVATE_KEY="<base64 Ed25519 private key>"
docker compose up --build
```

The Compose port is bound to loopback and runs the authority in development
mode. Put it behind an authenticated TLS reverse proxy before allowing remote
clients.

## Production requirements

Run `agent-comms-server` with:

- `AGENT_COMMS_ENV=production`;
- `AGENT_COMMS_DATABASE_URL` pointing to PostgreSQL;
- `AGENT_COMMS_SERVICE_KEY_FILE` pointing to a mode-0600 secret-mounted
  Ed25519 private key;
- `AGENT_COMMS_TLS_CERT` and `AGENT_COMMS_TLS_KEY`.

The service rejects production startup without TLS or an explicit signing
key. `/health/live` reports liveness, `/health/ready` checks PostgreSQL readiness, and
`/metrics` exposes Prometheus metrics. Graceful shutdown drains accepted HTTP
requests and stops the transactional outbox worker.

Connection-pool and admission limits are controlled with
`AGENT_COMMS_DB_MAX_CONNECTIONS`, `AGENT_COMMS_DB_MIN_CONNECTIONS`, and
`AGENT_COMMS_MAX_IN_FLIGHT`.

## Starting a team project

Use the authority URL and public half of the configured service signing key:

```sh
agent-comms init --mode service \
  --authority-url https://authority.example \
  --service-public-key "<base64 Ed25519 public key>"
```

Initialization creates the authoritative project and owner directly in
PostgreSQL, then writes the local service-mode bootstrap. Existing projects are
not converted in place.
