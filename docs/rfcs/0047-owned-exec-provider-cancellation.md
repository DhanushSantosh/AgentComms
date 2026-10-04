# RFC 0047: Bound owned exec-provider cancellation

## Status

Accepted, 2026-10-04, by the maintainer's explicit design approval.
Author: codex-main.

## Reproduced problem

On c7c486e, both the shared Codex/Claude CLI runner and the separate OpenCode
exec runner call `exec.CommandContext(...).Run()` with captured output writers.
An owned synthetic shell launches a two-second child that inherits its output.
A 100ms context deadline returns only after that child exits, about two seconds
later. Three uncached repetitions reproduce this on Codex and OpenCode paths.
Redirecting only child stdout/stderr removes the delay (about 100ms). Go's
command cancellation kills the direct process but the output-copy wait retains
the child's open pipes. A child without its own bound could stall indefinitely.

This contradicts RFC 0006's capped execution contract. The synthetic child is
self-terminating and launches no provider; no live runtime was killed. Evidence
currently proves the POSIX exec paths, not native Windows behavior or ACP/live
broker cleanup.

## Decision proposed

1. Introduce a repository-native lifecycle seam shared by the exec runners.
   Cancellation terminates only the process tree owned by this invocation and
   bounds output draining. No executable-name search, global kill or signalling
   unrelated provider/broker instances.
2. Use an owned process group on supported Unix platforms and an owned Windows
   job or equivalently proven handle-based supervisor. Establish ownership
   before child execution can escape it; do not rely on a late PID-only tree
   walk or assume cross-compilation proves Windows cleanup.
3. Bound post-cancellation output waiting with Go's `WaitDelay` or the shared
   supervisor's equivalent. Preserve bounded stdout/stderr, UTF-8 diagnostics,
   legitimate successful results, provider arguments, sandbox and permissions.
   Report cancellation as failure and drive the existing signed WAITING path;
   never publish partial output as a successful completion.
4. Apply to Codex/Claude exec and OpenCode exec. Review ACP and live-process
   lifecycle separately; shared brokers are not invocation-owned children and
   must not be killed when one worker invocation ends.
5. No new CLI option, event/schema change, history rewrite, provider login or
   host service installation. Document the post-deadline cleanup bound and
   any operating-system limits supported by actual evidence.

## Alternatives

- Only close output pipes: returns promptly but leaves child work running.
- Only kill the direct process: the reproduced failure persists.
- Kill by executable name or unvalidated PID tree: can affect unrelated agents.
- Raise deadlines: hides the defect without enforcing execution limits.

## Acceptance evidence

- Replace the opt-in audit regression with always-on native fixtures after
  implementation. Prove deadlines return within a fixed cleanup bound while
  an inherited-output child and nested child actually terminate.
- Test both exec runners, successful output, nonzero exits, large/truncated
  output, cancellation before start, and repeated cleanup.
- Prove an unrelated same-executable process remains live.
- Test the full worker claim/start/failure workflow: WAITING with a bounded
  reason, no result message or COMPLETED event after cancellation.
- Run native Linux, macOS and Windows fixtures and full candidate CI; retain
  exact outcomes. A POSIX shell reproducer is not Windows evidence.

## Open decision

The maintainer accepted the owned-tree cancellation and bounded-drain contract.
Final
platform implementation must pass the ownership-before-execution checks;
unavailable native proof remains a release blocker, not a waived check.
