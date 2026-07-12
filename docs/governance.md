# Governance

Roles set permission ceilings: Owner, Orchestrator, Agent, and Observer. Capabilities and repository/resource scopes narrow access. Principals are permanently classified HUMAN or AGENT; HUMAN approval cannot be supplied by an automated principal.

Assignments are expiring offers. Acceptance or eligible self-claim creates a four-hour protected lease. Overlapping write resources are rejected unless a governed shared-write exception exists. Heartbeats show liveness but cannot renew ownership. A renewal is durable and includes a progress summary. Stale work is never automatically reassigned. Handoffs retain ownership until acceptance.

Message obligations are per recipient: FYI delivers without acknowledgement; ACTION is accepted or rejected and then completed; CONTRACT requires all named parties; BLOCKER is acknowledged and resolved; DECISION requires read acknowledgement. Routine scoped work is autonomous. Scope changes, takeovers, shared writes, and contracts need orchestrator governance and affected acknowledgements. Destructive, irreversible, external, force-push, production-data, and credential actions require HUMAN approval.
