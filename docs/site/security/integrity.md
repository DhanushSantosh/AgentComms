---
title: Audit and integrity
description: Verify actor intent, authority receipts, immutable sequencing, and the limits of what the audit trail proves.
section: Security and trust
order: 2
audience: Security reviewers
lastVerified: 2026-08-01
related: [security/identity, reference/glossary]
---

Agent Comms uses dual attestation: actors sign intent before sequencing; the authority signs the committed event head after validation and persistence.

## Mutation boundary

The authority verifies the signed command, locks the project head, loads affected projections, revalidates authority and conflicts, appends the event, updates projections, advances the hash chain, and records an outbox item atomically.

The receipt binds project, sequence, event hash, actor-intent hash, and commit time. Git is never a mutation lock or success condition.

## Verification levels

```sh
agent-comms verify
agent-comms history --limit 100
agent-comms export jsonl
```

Verification can check actor intent, authority receipts, a sequence range, or the complete chain. JSONL export preserves event evidence for independent processing.

## What the record proves

- A valid actor signature proves possession of the corresponding private key and the canonical intent bytes.
- A valid authority receipt proves that the authority committed the event at the stated sequence and head.
- Hash-chain verification detects missing, reordered, or altered events.
- A claim proves runtime acknowledgement under current routing rules.
- Delivery evidence proves only the recorded connector actions.

## Idempotent retries

The same idempotency key and actor intent replay to the same committed result. A reused key with different intent is rejected. This protects callers that lose a response after the database commits.

## Key storage boundary

Personal-mode service signing keys and actor keys live in platform-private storage. Production service keys must be secret-mounted outside PostgreSQL. Diagnostics and audit exports never need private key material.
