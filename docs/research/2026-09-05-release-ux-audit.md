# AgentComms: release UX audit and improvement plan

Date: 2026-09-05. Author: Codex Main (`codex-main`). Task: `release-ux-audit-20260905`.

## Recommendation

Make the next release about **confident collaboration**: a person or agent can identify who they are, start useful work, understand what happened, and recover from a blocked step without reading implementation details.

The core is already substantial: governed state, semantic CLI output, a real terminal control room, structured agent interfaces, and signed releases. The largest remaining UX problem is that important information is often present in the underlying state but absent from the first screen or receipt. Fix that before expanding the command surface or redesigning the visual identity.

This audit identifies 16 improvement areas. Eight are high-priority release issues with reproduced behavior or a deterministic installer fixture. The remaining eight combine concrete source observations with proposed design improvements. Priority is UX/release priority, not vulnerability severity. This is a review and proposal, not implementation authorization or a claim that every possible defect has been found.

Read alongside [primary-source research](2026-09-05-cli-ux-primary-research.md) and [reproduction evidence](2026-09-05-release-ux-evidence.md).

## Baseline, method, and coverage

Reviewed `dev` at `93d0c35c763d4f3de5b2ac33387502988cb20a84`. The pre-existing `sites/landing/next-env.d.ts` change was preserved. The installed CLI reported `0.6.0-dev.45328b0`; behavioral findings below were rechecked with a fresh build of the current checkout. That build reports the default development version `0.1.0` and the current Git revision with a dirty suffix, because release version injection was not used and the checkout has the pre-existing generated-file change. It is not a test of the published v0.6.0 binary.

Inventory: the generated reference contains 125 visible command entries, including groups/root, 29 MCP tools, and 13 error codes. The TUI declares 18 views in five hubs. This inventory was used to cover the whole product surface; it does not mean every command/flag combination was executed.

Behavioral probes used an isolated personal project, isolated configuration, and file credentials under `/tmp/agentcomms-ux-audit-wxb4L0`. They did not register fixture identities or tasks in the real project. The real project's only coordination mutations were tracking this audit and communicating its scope. No application source was changed.

| Surface | Current assessment | Evidence in this audit |
| --- | --- | --- |
| Installation, alias, release/update | Good trust model; fallback and guidance need work | Shell/PowerShell installer source, update implementation, deterministic shell fallback fixture |
| First project and second-agent join | Works with guidance; insufficient readiness feedback | Fresh init, existing `.agents`, register, inactive write, activate, profile/status output |
| Help, navigation, command consistency | Grouped help is a strength; task examples and entity discovery are thin | Command reference inventory, help generation, live CLI help/errors |
| Tasks, ownership, leases | Strong model; protection and next action need clearer summaries | Create failure/success, claim, show, CLI/TUI task implementation |
| Messages and obligations | Several daily-use correctness issues | Real and fixture inboxes; recipient acknowledgement; repeated limits; plain/details output |
| Approvals and sensitive operations | Bound approval enforcement has useful UI information hidden by default | Generated contract approval, CLI summary/details, TUI reviewer source, existing tests |
| Invocations and runtime delivery | Durable request differs from executable work; summary parity is incomplete | No-consumer request, attention output, live TUI attention, delivery/inspector sources |
| Documents, decisions, artifacts, drafts, environment | Broad coverage; partial outcomes and editing need attention | Document notification failures, list output, form/source inspection |
| TUI navigation, forms, accessibility | Existing responsive and authority tests are valuable; keyboard/composition gaps remain | Live Linux PTY overview, palette, Down+Enter, task form; view/form/row-list sources and tests |
| Health, integrity, recovery, team authority | Diagnostic foundations exist; recovery instructions should be more specific | Live doctor, failure classifier, lifecycle/update/recovery docs; no induced production outage |
| MCP and automation | Stable envelope foundation; verify every multi-step outcome | MCP schema/docs inventory, JSON/quiet/error probes, shared output implementation |
| Landing, docs, demo, discovery | Good accessibility scaffolding; release alignment and fallbacks need checking | Landing/docs components, copy/search, install pages, content verification and browser-test source |

Verification completed: `go build` of the CLI and `go test ./internal/tui ./internal/cliui ./internal/onboarding ./internal/app` all passed. The TUI tests took about 59 seconds and app tests about 175 seconds. These are test-run durations, not user-facing latency measurements. Passing these tests did not cover the newly reproduced UX issues.

Limits: no native Windows/macOS install, fresh browser visual/accessibility run, assistive-technology session, real provider invocation, live team-mode outage, large-state benchmark, user interview, or production deployment was performed. Provider-specific Claude files/worktrees were excluded. Site conclusions are source/test observations, not a visual audit of the deployed pages. Those gaps become explicit release-validation work below.

## What should be preserved

- Grouped root help, `agc`, command-generated reference, and project-independent configuration/update commands already reduce friction. Do not propose them as missing features.
- Keep `--json` and stream envelopes stable while improving human presentation. Preserve sanitization, NO_COLOR handling, plain progress, and exit-code distinctions.
- Keep the difference between a delivered request, an acknowledgement, and completed work. Existing invocation details and the landing demo explain it; extend that consistency everywhere.
- Keep visible TUI actor/role, high-contrast theme, responsive scrolling, dynamic permitted actions, masked elevated-key entry, and stale-state indicators.
- Preserve exact-operation approvals, signed receipts, scope checks, protected verifier pins, and the install path that does not require users to install Cosign.
- Preserve the project's local-first and no-required-account experience. Do not introduce telemetry, login, or a hosted dependency merely to measure UX.

## Findings and proposed changes

### UX-01 — Initialization conflicts with an existing `.agents` directory

**P1 · reproduced · affected: first-time users integrating agent tooling.** Initialization fails even if `.agents/` is an empty directory. It asks the user to remove or rename it. The initializer owns `.agents` as a file, so a directory used by other tooling cannot coexist at that path. This was reproduced in the isolated fixture; the error is thrown before initialization.

Evidence: [runtime initialization](../../internal/runtimeinit/runtimeinit.go), `Initialize` and bootstrap creation; [init command](../../internal/app/cmd_core.go), `initCmd`.

Proposal: choose a non-conflicting bootstrap/discovery layout that can coexist with an existing directory. Preflight must explain exactly what will be created and why a conflict matters. Never suggest deleting nonempty user content as the default solution. Support explicit migration of the legacy bootstrap file, and detect already-initialized state as a resumable state rather than a dead end.

Acceptance: empty/nonempty `.agents` directories and legacy bootstrap files have tested outcomes; unrelated files survive; re-running setup points to the next incomplete step. **RFC needed:** managed-file/install discovery contract.

### UX-02 — Installer checksum fallback fails when `sha256sum` is unavailable

**P1 · deterministic shell reproduction · affected: systems requiring the `shasum` fallback.** `checksum_of` uses `sha256sum ... | awk ... || shasum ... | awk ...`. In POSIX shell the first pipeline returns the status of `awk`, so a missing/failing `sha256sum` can yield empty output with exit zero. The fallback never runs. A fixture returning 127 from `sha256sum` and a valid digest from `shasum` returned `checksum=<> exit=0`.

Evidence: [install.sh](../../install.sh), `checksum_of`, line 23. This demonstrates the fallback defect; it is not a claim that a native macOS installer was run.

Proposal: explicitly choose an available checksum tool, verify command success and digest shape, and fail with an actionable prerequisite error. Preflight Python 3 as well as curl; display the actual missing prerequisite. Reconcile the install page's “Nothing to install first” wording with its curl/Python requirement. After install, detect whether the destination is on PATH and provide shell-appropriate instructions.

Acceptance: fixture matrix for both checksum tools, only either tool, neither tool, failed hashing, missing Python, PATH missing, and a tampered verifier. A failed verification must never execute the verifier. Native supported-platform install remains a release gate. Preserve the current trust policy; no Cosign prerequisite.

### UX-03 — Registration and status do not explain readiness

**P1 · reproduced · affected: every new collaborating agent and its operator.** The registration receipt prints identity/profile/sequence but omits `PENDING`, the need for activation, and whether the active profile changed. In the fixture, the active profile remained the owner. Writing as the new agent failed with `active principal required`; the hint only said to check actor/profile/role/scopes. Ordinary `status` shows counts and integrity but not the selected identity, project path, or activation state.

Evidence: [registration](../../internal/app/cmd_agent.go), [status and init](../../internal/app/cmd_core.go), [profile resolution](../../internal/app/cmd_settings.go), [generic hints](../../internal/app/app.go).

Proposal: explicitly report “Registered; awaiting activation,” selected actor versus newly registered identity, who can activate, and a safe next command. Add a compact context/readiness header to status. Onboarding should finish with a first successful exchange, not merely a created database. Retain an agent's prohibition on silently granting itself elevated standing.

Acceptance: owner and agent can each explain their identity and next step from one screen; tests cover pending/suspended/active states, session-scoped identity, stale profiles, and no runtime. No implicit owner-to-agent profile switch during sponsored registration.

### UX-04 — Inbox presentation hides the information people came to read

**P1 · reproduced · affected: human CLI and plain-output consumers.** A modest subject disappears entirely from the inbox at the default 80-column width. It also disappears from redirected `--output plain`. The table renderer removes rightmost columns first, and subject is last. There is no `message show` command; users must discover `inbox --details` or JSON to read the message body.

Evidence: [message inbox](../../internal/app/cmd_message.go), [table renderer](../../internal/cliui/table.go), [capability defaults](../../internal/cliui/capabilities.go). The source does include SUBJECT; it is removed by presentation, not absent from the model.

Proposal: prioritize subject and sender over a long machine ID; wrap or abbreviate secondary fields with a clear expansion route. Add a focused message reader and a detail hint. Plain redirected output must retain data rather than silently adopting a guessed terminal width.

Acceptance: important text remains discoverable at 60/80/120 columns and with a long Unicode subject; redirected plain output preserves all columns or uses a documented lossless layout. Machine IDs remain exact in structured output. A new public reader command or output-contract change needs RFC review.

### UX-05 — Inbox filtering is not reliable for repeated use

**P1 · reproduced · affected: humans and agents polling work.** `--limit` trims a Go map before sorting, so the same unchanged inbox yielded eight different message IDs across twelve `--limit 1` calls. `--unread` checks aggregate message status, not the current recipient: after audit-agent acknowledged a two-recipient ACTION, that message still appeared unread because the other recipient remained pending.

Evidence: [inbox filtering](../../internal/app/cmd_message.go), `inbox.RunE`; [recipient projection](../../internal/projection/apply.go), `messageStatus`. See the exact fixture results in the evidence file.

Proposal: select a documented deterministic order before limiting; filter unread by current-recipient state. Keep “unread,” “unfinished obligation,” and “global message status” distinct. Decide separately whether FYI needs local read state; do not manufacture a durable acknowledgement obligation for FYI just to remove it from a list.

Acceptance: stable selection across repeated calls; two-recipient tests for ack/reject/complete; filter-plus-limit composition; clear ordering for custom IDs. If chronology needs new durable metadata, propose that migration separately rather than assuming IDs encode time.

### UX-06 — Default approval review omits the reviewed operation and expiry

**P1 · reproduced with a real generated bound approval · affected: reviewers.** `approval show` displayed tier, status, requester, action, and reason, but not expiry, affected principals, or the canonical reviewed operation. All were available under `--details`. A user following the natural “show then approve” workflow can miss the information the security design expects them to review.

Evidence: [approval show](../../internal/app/cmd_message.go); [TUI approval inspector](../../internal/tui/view.go), which already renders expiry and subject; [approval actions](../../internal/tui/approvals.go).

Proposal: make the operation summary, target/affected actors, validity, and intended consequence primary content. Keep digest and raw canonical JSON expandable. Format “expires in…” together with an exact timezone-bearing timestamp. Let users request a renewed approval from the original operation without retyping digests. Preserve the existing governed review/approval boundary.

Acceptance: default CLI/TUI review surfaces agree for contract, invocation, sensitive invocation, and takeover cases; expired, changed-payload, and consumed approvals cannot appear ready. This finding concerns informed review, not an assertion that cryptographic checks can be bypassed.

### UX-07 — Partial document notification failure looks like complete success

**P1 · reproduced · affected: scripts and agents coordinating decisions.** `document create --decision --notify nonexistent-agent --json` committed the document and returned `ok:true` with no structured warning; only stderr explained failed notification. With `--quiet`, even that warning disappeared. The caller cannot tell from the JSON receipt that the requested notification failed.

Evidence: [document creation](../../internal/app/cmd_artifact.go), `documentCmd` notification loop, and [shared warnings/receipts](../../internal/app/emit.go).

Proposal: return a structured per-recipient result and an explicit partial-outcome indicator; preserve critical warnings under quiet mode. Validate recipients early when possible, but still represent runtime failures after the first commit. Provide retry for failed notifications that does not duplicate the document or already-sent messages. Do not turn an already-committed document into a misleading “nothing happened” error.

Acceptance: all-success, partial-success, all-notification-failed, quiet, JSON, and retry tests. Freeze the contract and exit semantics through RFC review before changing machine-visible behavior.

### UX-08 — CLI attention can overlook work waiting for a consumer

**P1 · reproduced · affected: operators diagnosing why an agent has not started.** Requesting an invocation for an active agent with no runtime returns `PENDING_CONSUMER`, but the receipt leaves Consumer and Runtime blank and offers only a generic inspect hint. CLI `attention` counts `WAITING` invocations, not this PENDING request. In the same fixture, the TUI attention section did show pending delivery. `invocation inspect`'s default summary also omits instruction and result details.

Evidence: [invocation receipt/inspect](../../internal/app/cmd_invocation.go), [CLI attention](../../internal/app/cmd_core.go), [TUI attention](../../internal/tui/view.go), delivery summary in [service](../../internal/service).

Proposal: share one attention/readiness model across CLI/TUI/MCP. Distinguish “queued; no consumer observed,” “awaiting claim,” “approval required,” and “running” from actual evidence. For an ordinary queue, use a grace period before calling it a blocker. Explain that opening an inbox is not automatic delivery. Display the resolved policy default instead of a blank field and show the exact inspect/listen/setup path.

Acceptance: no-runtime, offline, pull-consumer, policy-gated, running, and completed cases agree across surfaces; healthy queues do not generate constant alarms. A stopped watcher never implies the background invocation was cancelled.

### UX-09 — Help coverage is broad but runnable workflow examples are scarce

**P2 · source and help inventory.** All visible commands have short help, which is good. However, only the root of the 125 generated command entries has an `example` field; `task create` does not mark its required title/branch/resources in flag metadata. A fixture omitting branch reached backend validation, which reported four generic required fields rather than identifying the missing one. Artifact and entity inspection also vary between `show`, `inspect`, and digest flags.

Evidence: [help construction](../../internal/app/app.go), [generated reference](../../sites/docs/src/generated/reference.json), [task flags](../../internal/app/cmd_task.go), [validation](../../internal/protocol/transitions.go), [documentation schema](../../internal/app/docs.go).

Proposal: add copyable examples to the ten primary journeys, required/default metadata, targeted validation, and entity completion where safe. Keep existing spellings stable; use cross-links instead of another broad rename. `--help` should explain how to read an inbox and get a task from open to complete without asking an assistant.

Acceptance: execute examples in isolated fixtures; distinguish unknown ID from wrong flag; completion returns quickly without mutation or requiring a healthy daemon. Compare [GitHub CLI completion](https://cli.github.com/manual/gh_completion).

### UX-10 — Claims do not clearly distinguish scope leases from worktree locks

**P2 · reproduced and source.** Claim help says it acquires a working-directory lock, but the path is optional. A fixture claim succeeded with an empty Worktree field. `task show` omits lease expiry and protected resources from its primary summary. Users can overestimate what was locked or miss an expiring lease.

Evidence: [task claim/show](../../internal/app/cmd_task.go), [work guide](../site/guide/work.md), [TUI task actions](../../internal/tui/tasks.go).

Proposal: report “scope lease acquired” and “worktree lock: not requested” separately. Show owner, protected resources, remaining lease, and renewal/start command. Make any inferred branch/path explicit and overridable; do not silently broaden locks.

Acceptance: scope-only and worktree claims have distinct receipts; conflict output names the conflicting task and lease; expired/near-expiry work is visible. Keep these advisory coordination protections distinct from filesystem access control.

### UX-11 — Command palette does not support conventional keyboard result selection

**P2 · live PTY reproduction and source.** Typing `/new` showed six matches; Down then Enter still opened the first, “new task.” The palette has no selection cursor and Enter always applies `matches[0]`. It accepts only single-byte key strings in its text path, also creating a source-level Unicode-input concern.

Evidence: [palette input](../../internal/tui/update.go), `updatePalette`; [matching](../../internal/tui/view.go), `paletteMatches`.

Proposal: use a proper text-input model plus selectable result index; Up/Down, Enter, Escape, and mouse should operate on the same focused match. Preserve aliases. Consider ranked matches and a full scrollable results view only after the keyboard contract is correct.

Acceptance: select the second or last result using only the keyboard; paste and Unicode input work; no keys leak into underlying views. This is a targeted interaction fix, not a reason to redesign all five hubs.

### UX-12 — Forms impose unnecessary transcription and lose unfinished input

**P2 · source-confirmed; basic task form exercised live.** Generic forms use single-line text inputs capped at 1,200 characters. Document update fields are blank rather than prefilled. TUI create forms require IDs that the CLI can generate. Required-field failure says only “Complete every required field.” Escape or clicking another hub clears form input without a dirty-state check. Invocation creation exposes fifteen fields at once, including advanced approval settings.

Evidence: [form construction](../../internal/tui/rowlist.go), `openActionForm`; [update/validation](../../internal/tui/update.go); [navigation discard](../../internal/tui/model.go); [documents](../../internal/tui/documents.go); [invocation form](../../internal/tui/invocations.go).

Proposal: prefill edit forms; generate IDs consistently; name and focus invalid fields; support multiline body/instruction editing. Show core fields first and reveal advanced routing/approval options when relevant. Preserve ordinary unsaved text through navigation or offer save/discard; never persist elevated-key passphrases in drafts. Use existing draft infrastructure where appropriate instead of inventing a second storage model.

Acceptance: edit one field without retyping a document; compose more than 1,200 characters without silent loss; recover a partially completed form; secrets never enter saved drafts. Confirm changes with the real event model.

### UX-13 — Documentation mixes released installation with unreleased behavior

**P2 · source-confirmed.** Install docs pin to latest released v0.6.0 while also promising the later `agc` alias; the changelog correctly lists that alias as Unreleased. The source-build guide says the binary will not have `agent-comms update`, but the fresh local build exposes that command. Compatibility onboarding docs still mention `invocation wait` after its rename to defer. A working link checker cannot detect these semantic mismatches.

Evidence: [install guide](../site/start/install.md), [changelog](../../CHANGELOG.md), [onboarding](../agent-onboarding.md), [update implementation](../../internal/app/cmd_update.go), [docs checks](../../sites/docs/scripts/verify-content.mjs).

Proposal: label stable versus next-release documentation explicitly, build command examples against the corresponding binary, and add a migration quick-reference. Explain sessions removed versus runtime session binding retained; decisions moved to tagged documents; alias availability by version. Keep protected version pins.

Acceptance: a user following stable install plus quickstart never hits an unreleased flag/alias; release automation checks semantic examples as well as links. Verify source-built self-update wording with the actual supported policy before editing it.

### UX-14 — Update and recovery should expose the affected projects and partial state

**P2 · source/contract review; no real upgrade performed.** Update defaults to reconciling all known projects; binary replacement precedes project reconciliation. Automatic reconciliation already isolates unrelated-project failures as warnings, but failures during update can require the user to infer whether the binary changed. Confirmation-required migrations stop the TUI before a guided upgrade view.

Evidence: [update pipeline](../../internal/app/cmd_update.go), [user reconciliation](../../internal/app/user_upgrade.go), [recovery guide](../site/operations/recovery.md), upgrade backlog.

Proposal: show current/target version and affected project list before work; retain separate “binary installed” and “projects reconciled” outcomes in errors/JSON. Add resumable repair guidance and a read-only upgrade-plan entry point accessible when normal TUI launch is blocked. Explain backup/rollback limits when schemas have changed.

Acceptance: interrupted download/replacement/migration and one-unhealthy-project fixtures have unambiguous outcomes; rerun is safe; no speculative success after remote uncertainty. Any change to upgrade scope/confirmation policy requires an RFC.

### UX-15 — Empty states and growing lists need action-oriented discovery

**P2 · source and empty-list probes; scale not benchmarked.** CLI empty lists show `(no rows)`; TUI row lists return “No rows here yet.” They do not distinguish “nothing created” from “nothing matching my filter” or suggest the next relevant action. Some CLI lists have filters, but there is no consistent filter/pagination experience across entities; TUI row-list management emphasizes navigation over text search.

Evidence: [CLI tables](../../internal/cliui/table.go), [TUI row lists](../../internal/tui/rowlist.go), task/message/invocation list handlers.

Proposal: domain-specific empty states; named current filters; reversible reset; stable list order; search on title/subject/actor as well as ID. Consider a read-only “my work” summary spanning tasks, obligations, and pending approvals, reusing the existing TUI view. Prioritize a small number of next actions over more dashboard counts.

Acceptance: new project, no matches, completed-only, and permission-limited fixtures read differently. Benchmark 100/1,000/10,000 records before selecting pagination/cache changes; do not claim a performance regression from source alone.

### UX-16 — Accessibility and website fallbacks need end-to-end release gates

**P2 · source/test review; assistive-tech and browser behavior not validated here.** Existing skip links, semantic controls, reduced-motion CSS, high contrast, and keyboard tests are strengths. Docs clipboard handlers await `writeText` without a failure path. The terminal needs explicit checks for plain output, focus, input retention, long text, and color-independent meaning; a visually clean screenshot or green Lighthouse run cannot establish usable screen-reader behavior.

Evidence: [docs clipboard](../../sites/docs/src/components/PlatformTabs.astro), [code-block copy](../../sites/docs/src/layouts/BaseLayout.astro), [terminal capability detection](../../internal/cliui/capabilities.go), [landing tests](../../sites/landing/tests/landing.spec.ts).

Proposal: copy failure feedback plus selectable-command fallback; terminal accessibility testing on the core journeys; visible shortcuts/selection; linear CLI alternative for every critical TUI workflow. Explain the web demo's simulated/local state so users never infer a real repository mutation. Verify search errors, mobile install, focus return, and reduced motion in-browser before claiming them complete.

Acceptance: practical keyboard and assistive-tech sessions, clipboard-denied fixture, narrow viewports, NO_COLOR/TERM=dumb, redirect/pipe, PowerShell and POSIX. The [W3C color principle](https://www.w3.org/WAI/WCAG22/Understanding/use-of-color.html) is useful guidance, not a TUI compliance certificate.

## The intended first-run and daily experience

Keep the public CLI stable where possible. The following describes behavior, not committed new command names:

1. **Install and verify:** exact release, platform, destination, PATH readiness, and verified artifact are clear.
2. **Initialize safely:** explain existing agent-tool files and preserve them; create project; show owner/context.
3. **Join and become ready:** register agent; explicitly show pending activation; human activates; select agent profile intentionally; verify the chosen transport.
4. **First collaboration:** send/read a message or request work; show who must act and whether a consumer is present.
5. **Work with protection:** create a narrowly scoped task; claim it; show lease and any worktree lock; renew with progress.
6. **Review safely:** display the exact operation, actors, expiry, and consequence; preserve the separate authorization step.
7. **Finish with evidence:** distinguish durable completion from partial notification or delivery; link the result and next action.

An illustrative improved status screen:

```text
AgentComms · AgentComms repository · personal authority
Acting as codex-main · Senior Engineer · ACTIVE
State verified · observed just now

Your work: 1 task in progress · lease expires in 3h 40m
Inbox: 2 obligations awaiting your response
Delivery: 1 request queued; no consumer observed

Next: inspect the queued request to choose a consumer or continue polling.
```

The counts and wording above are a design example, not the live repository's status. Exact actor, freshness, and obligation semantics must come from shared state, not presentation guesses.

## Prioritized implementation plan

These packages are proposals. No RFC is marked accepted, no implementation is started, and no other agent is assigned ownership by this document. Effort is relative engineering size: S = localized, M = cross-surface, L = protocol/lifecycle or substantial interaction work. Validate estimates after design review.

| Wave | Deliverable | Findings | Size | Exit condition |
| --- | --- | --- | --- | --- |
| A — release correctness | Fix installer fallback; readable inbox and deterministic recipient filters; primary approval content; explicit partial notification results | 02, 04–07 | M overall | Every reproduced scenario has a failing-before/passing-after test; JSON contracts reviewed |
| B — first useful collaboration | Coexisting/resumable setup, context/readiness, exact recovery hints, no-consumer visibility, example-driven first task | 01, 03, 08–10 | L | Two fresh participants complete the seven-step journey without manual config edits |
| C — daily terminal use | Keyboard palette, progressive forms, prefilled edits, unsaved-input handling, contextual empty states/search | 11, 12, 15 | L | Keyboard-only daily journey succeeds at 80×24; long content preserved |
| D — release validation | Stable/next docs, cross-platform installer checks, upgrade partial outcomes, browser/terminal accessibility, scale baseline | 13, 14, 16 plus all | M–L | Signed release candidate passes the documented journey matrix, including failure paths |

Run documentation and native platform checks alongside earlier waves rather than waiting until the end. Correct the published install/version claims early. Keep changes in reviewable packages; do not combine output redesign, schema migration, and navigation overhaul in one patch.

Suggested implementation boundaries for the team: CLI presentation/filtering; terminal interaction; bootstrap/installer/lifecycle; release documentation and journey tests. Shared receipt, readiness, and action metadata should be designed once and consumed across CLI/TUI/MCP. Coordinate file ownership before parallel implementation: `cmd_message.go`, `emit.go`, and TUI model/form code are likely overlap points.

RFCs are appropriate for the bootstrap path, machine-visible partial outcomes, new public commands, upgrade behavior, or major navigation changes under [the project's RFC rules](../rfcs/README.md). Straightforward fixes restoring intended output/filter/keyboard behavior should reuse existing contracts where possible. A roadmap is not a reason to relax approval security or remove version pinning.

## Release validation and measurement

| Journey | Required cases | Proposed acceptance |
| --- | --- | --- |
| Install | Linux/macOS/Windows, clean shell, missing dependency, PATH absent, checksum-tool alternatives, tampering | Installs successfully or explains the exact recoverable prerequisite; never executes unverified content |
| Join | New repo, existing `.agents`, owner, agent pending/active, session/profile mismatch | Identity and next step visible; unrelated files preserved; no implicit privilege gain |
| Coordinate | Multiple recipients, long subjects, custom IDs, repeated limits, no consumer, disconnected consumer | Stable results, correct per-recipient obligations, truthful delivery state |
| Work | Scope-only claim, worktree lock, conflict, renewal, handoff, review, completion | Every actor can identify what is protected, who owns it, and what to do next |
| Approve | Bound payload, expiry, consumption, denial, rejected request | Default review surface is sufficient to understand the operation; enforcement unchanged |
| Automate | JSON/plain/quiet, closed stdin, cancellation, partial success, unknown remote result | Parseable contracts and documented exit semantics; no silently discarded critical outcome |
| Recover | Offline team cache, unavailable authority, interrupted upgrade, mixed project versions | Freshness and partial progress explicit; no destructive guesswork in retry guidance |
| TUI/web | Keyboard only, 80×24 and 60×20, long/Unicode text, reduced motion, copy denied, assistive tech | Core tasks possible with visible focus and recoverable input; no color-only essential state |

Before choosing numeric product targets, observe a small formative group of human operators and agent users. Record time to first successful exchange, failed commands, help lookups, completion rate, wrong-actor attempts, and recovery success. Start with 5–8 participants as a qualitative discovery exercise, not statistically representative proof. Collect locally or with explicit consent; this plan does not authorize telemetry.

Proposed release goals to calibrate against that baseline: first successful collaboration within ten minutes after prerequisites are installed; no unexplained failure in the primary journeys; no required source inspection; every failed/partial mutation has a useful next step. Add performance budgets only after measuring large-state behavior—initial candidates are subsecond local summaries and responsive keyboard feedback, not claimed current measurements.

## Ideas worth exploring after the release fixes

- An opt-in guided “first collaboration” fixture or demo that uses disposable project state and makes the boundary obvious.
- Context-sensitive suggested actions based on real permission/state checks, with the equivalent CLI command visible for learning.
- Saved personal inbox/task filters and a concise daily work summary; avoid new globally shared actor state.
- Searchable human-friendly labels alongside exact durable IDs; never shorten identifiers in machine receipts.
- Reusable team onboarding/connectivity profiles, validated before declaring a runtime ready; no implicit execution or elevated activation.
- Consistent outcome/watch views across tasks, approvals, and invocations, built on existing wait/stream semantics rather than another lifecycle.

Avoid a theme rewrite, another broad CLI rename, automatic elevated approvals, mandatory Cosign installation, or new collaboration protocols as prerequisites for this release. The evidence points first to information visibility, recovery, consistency, and user control.

## Research rationale

Git separates stable script output from evolvable human output; GitHub CLI exposes structured formatting and explicit accessibility controls. These support keeping AgentComms' existing machine contract while improving the first screen people read. [Git status](https://git-scm.com/docs/git-status), [GitHub CLI formatting](https://cli.github.com/manual/gh_help_formatting), [GitHub CLI environment](https://cli.github.com/manual/gh_help_environment).

The companion research adds official examples for account status, completion, exit codes, dry-run/diff, bounded waiting, and progress. The recommended AgentComms designs are our synthesis of that research and the local evidence, not claims that copying another tool will automatically improve usability.
