# Security policy

Report suspected vulnerabilities privately through GitHub Security Advisories. Do not open a public issue containing exploit details or credentials.

Maintainers aim to acknowledge credible reports within three business days. Validation and fix development occur privately, disclosure is coordinated with the reporter, and fixes are released before detailed publication. CVEs and contributor credit are provided when appropriate.

The cooperative trust model protects integrity with actor-bound signatures, an immutable hash chain, scoped authorization, and atomic Git transactions. Hostile processes running as the same operating-system account are outside the isolation boundary. Agent Comms contains no telemetry and never stores private actor keys in project history.

Before v1, the newest preview line receives best-effort fixes and may require schema migration. After v1, the current stable minor is supported, and the previous stable minor receives critical security fixes for six months after replacement.
