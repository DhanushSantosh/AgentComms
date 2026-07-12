# Threat model

Agent Comms coordinates cooperative principals sharing one filesystem and OS trust boundary. It defends against accidental conflicts, unauthorized governed operations, corrupted or reordered durable history, forged actor records without a matching key, interrupted writes, and unsafe checkpoint divergence.

It does not defend against a hostile administrator, kernel compromise, or a malicious process with unrestricted access to the same OS account and keyring. Git remotes are recovery transport, not distributed locks. Human-tier policy is an auditable governance control, not biometric proof.
