# Governance

Message kinds are FYI, ACTION, CONTRACT, BLOCKER, and DECISION. ACTION, CONTRACT, BLOCKER, and DECISION records remain open until their required acknowledgement or rejection transition. Routine in-scope work is autonomous. Scope takeover, shared writes, and contracts require orchestrator approval and affected acknowledgements. Irreversible, destructive, external, force-push, production-data, and credential actions require human approval.

Completed work remains active for seven days before an archive event can move it from active projections; history is never deleted. Summaries are limited to 1,200 characters. Evidence is SHA-256 addressed, and the default inline threshold is 5 MiB; larger evidence requires configured Git LFS or external storage.
