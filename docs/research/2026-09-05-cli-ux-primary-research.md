# AgentComms release UX: primary-source research

Date: 2026-09-05. Scope: CLI/TUI developer-tool patterns from official documentation, translated into release recommendations. This is external research, not a claim that any proposed capability is missing or broken in AgentComms. The accompanying local audit must determine existing coverage before implementation. Proposed command spellings below are illustrative, not verified current commands.

## Recommendation

Make the release promise: **a human or agent can tell what context they are in, what happened, what is waiting, and what to do next, without interpreting terminal animation or reading implementation details.** Prioritize coherent end-to-end journeys over a larger command inventory. Preserve established command and machine-output contracts while improving human presentation.

The strongest comparison is not a single competing product: Git provides a model for stable script output and safe previews; GitHub CLI for discoverability, identity diagnostics, and accessible terminal controls; kubectl for explicit waits and proposed-state inspection; Docker for adapting progress to terminal capabilities.

## 1. One visible context and onboarding checkpoint

**Evidence.** `gh auth status` identifies the active account for each host, tests authentication, and reports issues. Its documentation explicitly distinguishes ordinary exit behavior from JSON behavior: JSON normally exits zero even when accounts have authentication problems. [GitHub CLI authentication status](https://cli.github.com/manual/gh_auth_status)

**AgentComms recommendation.** Provide one concise readiness view covering repository/project, local versus team authority, selected identity and role, activation state, daemon connectivity, and outstanding setup steps. Each item needs observed state and one appropriate recovery action; avoid a generic “not initialized” answer for every failed dependency. Never print bearer tokens or secret key material. If a check could not run, label it “not checked,” not “healthy.”

Design setup as resumable checkpoints: initialize project → select/register identity → activate if required → verify connectivity → send a first message. The “registration submitted” screen should clearly distinguish pending activation from usable identity. The final screen should confirm readiness using the same checks as the everyday status view.

**Release test.** A new user can identify the active project/identity and explain a pending activation without opening a configuration file. An interrupted setup can resume without duplicate identities. Decide and document whether JSON diagnostic output encodes unhealthy state through exit status, fields, or both; do not inherit GitHub CLI's exception accidentally.

## 2. Discover workflows before advanced nouns

**Evidence.** GitHub CLI generates completion scripts for Bash, Zsh, Fish, and PowerShell, and documents package-manager and manual installation paths. [GitHub CLI completion](https://cli.github.com/manual/gh_completion)

**AgentComms recommendation.** Make root help a short map of user goals: get started, coordinate work, review requests, inspect health, automate. Retain a complete reference beneath command-specific help. Put a runnable first example and the nearest next command beside each common workflow. Completion should cover canonical commands and aliases consistently; decide explicitly which dynamic completions require a running daemon and provide a fast fallback when unavailable.

Avoid renaming commands solely to improve taxonomy immediately before release. Prefer grouped help, documented aliases, consistent flag vocabulary, and clear migration notes. Review examples in both POSIX shells and PowerShell rather than assuming quoting is portable.

**Release test.** Given only `--help`, a newcomer finds how to read an inbox, claim work, and inspect an approval. Completion setup is documented for every supported shell, and completion never mutates project state.

## 3. Freeze an automation contract; let human output evolve

**Evidence.** Git explicitly guarantees porcelain status stability across versions and user configuration, while its long human-readable format can change. [Git status](https://git-scm.com/docs/git-status)

**Evidence.** GitHub CLI exposes selectable JSON fields, jq filtering, and templates separately from default human output. [GitHub CLI formatting](https://cli.github.com/manual/gh_help_formatting)

**AgentComms recommendation.** Treat structured output as a public integration interface. Inventory every command used by agent integrations and document consistent success, empty, pending, and failure shapes. Preserve existing schema compatibility; if changes are necessary, provide versioning or an explicit migration rather than silently changing fields. Human summaries, warnings, and progress must not contaminate JSON stdout.

For mutation receipts, expose the durable entity/event identifier and distinguish accepted, applied, and completed where those are actually different lifecycle states. A machine client should not need to match prose such as “sent successfully.” State clearly whether empty lists are successful results and whether an accepted asynchronous request returns before work completes. JSON does not by itself guarantee a stable contract.

**Release test.** For important commands, capture stdout/stderr and exit code under TTY, redirected output, empty state, and error conditions. Parse machine stdout as the documented format; verify no ANSI escapes or extra human lines. Existing integrations keep working after wording and color changes.

## 4. Errors should identify the recovery, not only the failure

**Evidence.** GitHub CLI publishes exit codes for success, failure, cancellation, and authentication-required states; individual commands can define additional codes. [GitHub CLI exit codes](https://cli.github.com/manual/gh_help_exit-codes)

**AgentComms recommendation.** Standardize errors around a stable code, a plain-language cause, relevant context, and a safe next action. Distinguish inactive identity, denied capability, absent approval, expired approval, lease conflict, unreachable authority, and malformed input. Include an entity ID or correlation ID when it helps investigation; redact secrets from diagnostic context.

Example proposed wording: “Cannot claim task T42: agent A7 owns an overlapping lease until 14:32 UTC. Inspect the conflicting task or ask its owner to release it.” Do not suggest takeover or weakening policy as the default fix for a normal conflict. For remote timeouts, state whether acceptance is known, rejected, or unknown before suggesting retry; unknown mutation outcomes require an idempotent retry or a status lookup.

**Release test.** A denied request and a transport outage produce distinct codes and actions. Help text lists stable exit semantics. Cancellation, timeout, and domain failure are testable without parsing English prose.

## 5. Preview consequential changes with resolved targets

**Evidence.** `git clean --dry-run` displays what would be removed without removing it. [Git clean](https://git-scm.com/docs/git-clean)

**Evidence.** `kubectl diff` compares current configuration with what application would produce, and distinguishes no differences, differences, and failures in its exit codes. [kubectl command reference: diff](https://kubernetes.io/docs/reference/generated/kubectl/kubectl-commands#diff)

**AgentComms recommendation.** For project deletion, broad cleanup, role/capability changes, and takeover, show the resolved project, identity, target set, and consequences before execution. A preview is useful only when it applies the real selection and validation rules. Identify any checks that cannot be guaranteed until execution; revalidate authority and state when committing the operation.

Keep noninteractive use possible through explicit flags and documented input. Avoid universal confirmation prompts for ordinary reversible operations; reserve stronger confirmation for substantial destructive consequences. Confirmation must not silently bypass authorization. Review policy-sensitive previews against the existing RFCs before designing new semantics.

**Release test.** A preview reports exact resolved targets without creating events or changing state. A changed target or expired approval between preview and execution cannot slip through. Scripts cannot hang on a prompt when stdin is closed.

## 6. Make asynchronous coordination observable

**Evidence.** `gh run watch` follows a run to completion, offers compact relevant/failed-step output, and has an option to return failure through exit status. [GitHub CLI run watch](https://cli.github.com/manual/gh_run_watch)

**Evidence.** `kubectl wait` supports conditions, creation/deletion waits, and explicit timeout durations. [kubectl wait](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_wait/)

**AgentComms recommendation.** Define a consistent distinction between “request accepted,” “waiting for approval,” “queued for delivery,” “running,” and terminal outcomes, but only expose states the underlying protocol can substantiate. After a mutation, return the receipt plus how to inspect or wait for that entity. Consider a common wait/watch experience across invocations, approvals, and tasks, reusing current commands when available.

A useful team summary answers: who owns work, what is blocked, who must act, how long it has been waiting, and when data was last refreshed. Include disconnected/stale state explicitly. Compact mode should emphasize state changes and blockers; verbose mode can retain full event history. Exiting a watcher should say whether background work continues. A lost connection must not look like successful completion.

**Release test.** Exercise approval pending → approved → running → complete, rejection, timeout, disconnect/reconnect, and cancellation. CLI, TUI, and structured output agree on the same underlying state. A user can find the responsible actor for a blocked task without reading raw event payloads.

## 7. Treat plain terminals and accessibility as first-class surfaces

**Evidence.** Docker Buildx separates TTY redraws, plain progress, quiet output, and JSON-line progress, with automatic terminal detection. [Docker Buildx progress modes](https://docs.docker.com/reference/cli/docker/buildx/build/#progress)

**Evidence.** GitHub CLI documents `NO_COLOR`, accessible colors and prompts, disabled prompting, and a textual alternative to animated spinners. Some accessibility controls are labeled preview, so they are examples rather than universal compatibility guarantees. [GitHub CLI environment controls](https://cli.github.com/manual/gh_help_environment)

**Evidence.** W3C's use-of-color criterion requires an additional means of conveying information beyond color. It is a web accessibility criterion; using its principle in terminals does not establish WCAG compliance for a TUI. [W3C: use of color](https://www.w3.org/WAI/WCAG22/Understanding/use-of-color.html)

**AgentComms recommendation.** Offer text labels for every colored status; preserve important information with color disabled and without Unicode glyph support. Keep a plain, linear CLI alternative for core TUI workflows. Provide visible focus, discoverable keyboard actions, readable wrapping at narrow widths, predictable cancellation, and an animation-free progress option. Avoid unbounded redraw traffic in logs or screen-reader output.

Test Windows Terminal/PowerShell, a common POSIX terminal, redirected output, low-color terminals, a narrow viewport, and a screen-reader workflow. Terminal accessibility must be evaluated through real task completion; screenshot quality alone does not test reading order or announcements.

**Release test.** A reviewer can identify and act on a pending approval without color or animation. Redirected status/watch output stays linear and readable. Every consequential TUI action has an accessible CLI route and the same authorization semantics.

## Proposed release sequence and evaluation

These are prioritization judgments from the research, not confirmed local defects or externally measured performance targets.

| Sequence | Work package | Evidence needed before calling it complete |
| --- | --- | --- |
| 1 | Inventory current command/output/error contracts and map five user journeys | Baseline recordings and checks for setup, second-agent join, task coordination, approval, recovery |
| 2 | Correct misleading readiness, receipt, error, and stale-state presentation | Failure-path tests and agreement across CLI/TUI/machine state |
| 3 | Strengthen automation and destructive-operation consistency | Contract compatibility tests, noninteractive runs, side-effect-free previews |
| 4 | Improve grouped help, onboarding next actions, focused team overview | Short observed user sessions; compare task success and recovery with baseline |
| 5 | Complete terminal/accessibility and cross-platform polish | Practical keyboard/screen-reader/plain-output checks on the supported matrix |

Suggested acceptance targets to agree with maintainers: every primary workflow has an understandable next step; every noninteractive workflow completes or fails without prompting; every mutation has an unambiguous receipt; every asynchronous view distinguishes stale observation from current state; no major-release wording change breaks an agent parser. Measure first successful collaboration time, failed attempts, help lookups, and recovery success before selecting numerical targets.

## Boundaries

This research does not validate local implementation, claim all upstream designs are ideal, or recommend copying their exception cases. Verify actual AgentComms behavior and existing tests before opening implementation work. No competitor usability studies or user interviews were performed. The 12 official references linked above establish concrete documented patterns; the AgentComms designs and release sequence are the author's synthesis.
