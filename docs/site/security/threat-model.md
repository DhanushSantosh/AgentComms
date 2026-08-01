---
title: Threat model
description: Know which coordination and integrity failures Agent Comms addresses and which operating-system compromises remain outside its boundary.
section: Security and trust
order: 3
audience: Security reviewers
lastVerified: 2026-08-01
related: [security/identity, security/integrity]
---

Agent Comms coordinates cooperative principals sharing a filesystem and operating-system trust boundary.

## In scope

The protocol addresses:

- accidental overlapping work and stale leases;
- unauthorized governed transitions;
- forged actor records without the matching key;
- corrupted, reordered, missing, or partially committed history;
- duplicate commands after timeouts or lost responses;
- false delivery success and ineligible runtime claims;
- unsafe multi-host coordination through Git remotes;
- unbounded request, result, cursor, queue, and connection pressure.

## Outside the boundary

It does not defend against a hostile administrator, kernel compromise, or a malicious process with unrestricted access to the same OS account and ordinary keyring.

The elevated key is a narrow exception: sensitive identity and human-approval transitions require a passphrase that is not stored. This protects against a live process merely inheriting OS access, but not a process that captures the passphrase or compromises the primary key and convinces the human to enroll another elevated key.

Human-tier policy is an auditable governance control, not biometric proof. Display names are cosmetic; IDs, roles, keys, and fingerprints are authoritative.

## Operational assumptions

- Interactive PTY delivery is local-host only.
- One interactive terminal is dedicated to one supervised runtime.
- Production services use TLS, authentication, bounded pools, and secret-mounted keys.
- Operators protect backups, keyrings, connector configuration, and provider credentials.
- Agents remain responsible for reviewing untrusted instruction content even when transport is authentic.
