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

```sh
agent-comms draft save --id release-notes --kind document --body '{"title":"Draft"}'
agent-comms draft list
agent-comms draft show --id release-notes
agent-comms draft delete --id release-notes
```

Drafts are capped per project: 1,000 drafts, 50 MiB total, and 5 MiB for any single draft. Drafts do not expire, so reaching a cap makes the next `draft save` fail with `local draft count limit reached` or `local draft storage limit reached` until you free space. `draft delete` is how you free it — removing a draft immediately releases both the count and the bytes it held. Deletion is scoped to one draft in the current project; a draft ID that does not exist is reported as an error rather than passing silently, so a mistyped ID never looks like a successful cleanup.
