---
title: Documents and artifacts
description: Keep durable project knowledge, content-addressed outputs, environment entries, and private local drafts distinct.
section: User guide
order: 6
audience: Everyone
lastVerified: 2026-08-01
related: [security/integrity, guide/maintenance]
---

Agent Comms separates governed records from local preparation so an unfinished draft never becomes project truth by accident.

## Documents

```sh
agent-comms document create \
  --id runbook-auth \
  --title "Authentication runbook" \
  --body-file docs/auth-runbook.md \
  --tag security \
  --tag operations

agent-comms document show --id runbook-auth
agent-comms document list
```

Updates append a new signed version. Superseding a document points readers at its replacement without deleting the old content.

## Artifacts

```sh
agent-comms artifact add --path dist/report.json
agent-comms artifact show --sha256 <digest>
agent-comms artifact verify --sha256 <digest>
```

Artifacts are copied into content-addressed storage under their SHA-256 digest. Project policy bounds artifact size. Verification recomputes the stored digest.

## Environment entries

```sh
agent-comms env set API_BASE_URL https://staging.example.test
agent-comms env get API_BASE_URL
agent-comms env list
agent-comms env delete API_BASE_URL
```

Environment entries are governed coordination data. Do not store secrets unless the project's threat model and storage controls explicitly permit it.

## Local drafts

Drafts are bounded, local, and non-authoritative. Use them for document text, message bodies, or artifact metadata that is not ready to submit. A draft creates no event, lease, obligation, or current truth until submission succeeds.
