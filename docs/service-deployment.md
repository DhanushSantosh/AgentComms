# Service deployment

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

From the legacy project root, use the server public key printed by a
development server or provisioned alongside the production private key:

```sh
agent-comms migrate service \
  --authority https://authority.example \
  --service-public-key "<base64 Ed25519 public key>" \
  --migration-token "$AGENT_COMMS_MIGRATION_TOKEN"
```

Migration verifies and locks the complete legacy history, records the legacy
Git head, resumes idempotent batches, compares server and local projections,
stores a server-signed import receipt, and switches the bootstrap atomically.
After cutover, legacy writes are rejected and cached reads are served by the
per-user daemon.
