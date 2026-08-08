---
title: Deploy the team service
description: Run the PostgreSQL authority for multi-host coordination and initialize a service-mode project securely.
section: Team operations
order: 1
audience: Operators
lastVerified: 2026-08-01
related: [start/modes, operations/recovery, security/integrity]
---

Deploy team mode only when coordination crosses users or machines. A single workstation with many agents should stay in personal mode.

## Development with Compose

The repository includes `compose.yaml` and `Dockerfile.server`:

```sh
export POSTGRES_PASSWORD="$(openssl rand -hex 24)"
export AGENT_COMMS_SERVICE_PRIVATE_KEY="<base64 Ed25519 private key>"
docker compose up --build
```

The development service binds to loopback. Do not expose it remotely without authenticated TLS termination.

## Production contract

Run `agent-comms-server` with:

- `AGENT_COMMS_ENV=production`;
- `AGENT_COMMS_DATABASE_URL` pointing to PostgreSQL;
- `AGENT_COMMS_SERVICE_KEY_FILE` pointing to a mode-0600 secret-mounted Ed25519 key;
- `AGENT_COMMS_TLS_CERT` and `AGENT_COMMS_TLS_KEY`.

Production startup rejects an insecure default key or missing TLS. Keep service signing keys outside PostgreSQL.

Bound database and admission pressure with `AGENT_COMMS_DB_MAX_CONNECTIONS`, `AGENT_COMMS_DB_MIN_CONNECTIONS`, and `AGENT_COMMS_MAX_IN_FLIGHT`. Put authentication, request logging policy, and network access control in front of the service.

## Initialize a team project

```sh
agent-comms init --mode service \
  --authority-url https://authority.example \
  --service-public-key "<base64 Ed25519 public key>"
```

Initialization creates the project and owner in PostgreSQL, then writes the local service bootstrap. Existing personal projects are not converted in place.

## Health surfaces

- `/health/live` reports process liveness.
- `/health/ready` verifies PostgreSQL readiness.
- `/metrics` exposes Prometheus metrics.

Graceful shutdown drains accepted HTTP requests and stops the transactional outbox worker.
