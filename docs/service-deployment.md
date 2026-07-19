# Team service deployment

This deployment is needed when users or agents coordinate across multiple
machines. Local-only projects should use the default personal mode and do not
need this stack.

The authority is a stateless Go service backed by PostgreSQL. PostgreSQL is
the source of truth; Git is retained only as migration evidence and an
optional asynchronous audit target.

## Development deployment

Set three secrets and start the supplied Compose stack:

```sh
export POSTGRES_PASSWORD="$(openssl rand -hex 24)"
export AGENT_COMMS_MIGRATION_TOKEN="$(openssl rand -hex 32)"
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
- `AGENT_COMMS_TLS_CERT` and `AGENT_COMMS_TLS_KEY`;
- a high-entropy `AGENT_COMMS_MIGRATION_TOKEN`.

The service rejects production startup without TLS or an explicit signing
key. `/health/live` reports liveness, `/health/ready` checks PostgreSQL readiness, and
`/metrics` exposes Prometheus metrics. Graceful shutdown drains accepted HTTP
requests and stops the transactional outbox worker.

Connection-pool and admission limits are controlled with
`AGENT_COMMS_DB_MAX_CONNECTIONS`, `AGENT_COMMS_DB_MIN_CONNECTIONS`, and
`AGENT_COMMS_MAX_IN_FLIGHT`.

## Migrating a project

From a personal- or legacy-mode project root, use the server public key printed
by a development server or provisioned alongside the production private key:

```sh
agent-comms migrate service \
  --authority https://authority.example \
  --service-public-key "<base64 Ed25519 public key>" \
  --migration-token "$AGENT_COMMS_MIGRATION_TOKEN"
```

Legacy migration verifies and locks the complete signed filesystem history.
Personal migration verifies the SQLite event chain and every project-scoped
authority receipt. Both paths resume idempotent batches, compare server and
local projections, store a new server-signed import receipt, and switch the
bootstrap atomically. After cutover, the prior authority remains read-only
migration evidence and cached reads are served by the per-user daemon.
