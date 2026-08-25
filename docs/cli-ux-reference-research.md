# CLI UX reference research: OpenCode and OpenClaw

Research date: 2026-08-25. Sources are limited to first-party documentation and official source repositories. Because both projects evolve quickly, links point to the current official documentation or repository branch; pin them when converting this research into a long-lived specification.

## Executive takeaways

1. **Human output and machine output are different products.** The normal mode should be concise, structured, and attractive; `--json` must be a strict, stable data contract with no decoration on stdout.
2. **TTY awareness should be centralized.** Color, animation, spinners, and hyperlinks belong only in an interactive terminal. Piped output should remain clean and predictable.
3. **Commands should return semantic results, not print arbitrary objects.** A shared presentation layer can render the same result as a summary, table/detail view, JSON, or eventually another format.
4. **Use progressive disclosure.** Show the useful summary first, provide `--verbose`/`--details` for diagnostics, and preserve raw data through `--json`.
5. **Treat stderr/stdout and exit codes as API boundaries.** Data belongs on stdout; status, progress, warnings, and diagnostics belong on stderr; failures must exit non-zero.

## OpenCode

### Interaction model and discoverability

- Running `opencode` with no subcommand starts its TUI; `opencode run` is the explicit non-interactive path for scripts and one-shot requests. The CLI also exposes focused command families such as `models`, `session`, `stats`, `export`, `import`, `mcp`, `serve`, and `debug` ([official CLI reference](https://opencode.ai/docs/cli/)).
- The TUI provides slash commands, a command palette/keybindings, fuzzy `@` file references, direct shell execution with `!`, session continuation, and a `/details` toggle for tool-execution detail. These are strong progressive-disclosure and command-discovery patterns ([official TUI documentation](https://opencode.ai/docs/tui/)).
- Custom commands include descriptions that appear in the TUI command picker. Commands can bind an agent, model, or subtask behavior, so customization remains discoverable rather than becoming hidden config ([official commands documentation](https://opencode.ai/docs/commands/)).
- Destructive uninstall behavior is explicit: dry-run, confirmation, keep-config/keep-data, and force flags are independently represented ([official CLI reference](https://opencode.ai/docs/cli/)).

### Human-readable output

- The source defines a small semantic style vocabulary—highlight, dim, warning, danger, success, and info—rather than scattering arbitrary color choices. Errors are normalized as a bold red `Error:` label followed by the message ([official `ui.ts`](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/cli/ui.ts)).
- `opencode run` renders a compact agent/model heading, uses recognizable glyphs for tool state, shows concise inline tool summaries, and expands output into blocks only when useful. Failed tools receive a cross glyph and an explicit `failed` label ([official `run.ts`](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/cli/cmd/run.ts)).
- Warnings use an emphasized `!` plus plain explanatory text, including recovery/fallback information (for example, when an agent cannot be used). This makes severity redundant across color and text/glyph, which remains understandable without color ([official `run.ts`](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/cli/cmd/run.ts)).
- List-oriented commands expose a formatted table as the human default and JSON as an alternate format; database queries expose JSON or TSV where data export is the primary purpose ([official CLI reference](https://opencode.ai/docs/cli/)).

### JSON and scriptability

- `opencode run --format json` emits newline-delimited event objects. Each object includes a type, timestamp, session ID, and event-specific data, while default mode translates those events into human-readable output ([official `run.ts`](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/cli/cmd/run.ts)).
- This is a useful distinction: streaming work naturally maps to JSON Lines/events, while bounded listing commands can return one JSON value. The CLI documentation explicitly positions `run` as suitable for scripting and supports stdin input and attachment to a long-running server ([official CLI reference](https://opencode.ai/docs/cli/)).
- Logs can be routed to stderr with `--print-logs` and filtered by `--log-level`, keeping normal output separate from diagnostics ([official CLI reference](https://opencode.ai/docs/cli/)).

### Configuration and visual identity

- TUI behavior has its own schema-backed JSON/JSONC configuration, including theme, leader key, command-list binding, scrolling, and acceleration. Users can therefore tune interaction without altering the semantic output contract ([official TUI documentation](https://opencode.ai/docs/tui/)).
- The non-TUI CLI owns a recognizable wordmark/logo and terminal-aware rendering. The code selects a simpler representation when neither stdout nor stderr is a TTY ([official `ui.ts`](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/cli/ui.ts)).

### Architecture and libraries

- OpenCode's command router is built on `yargs`, with strict argument parsing, wrapped help, aliases, completion generation, centralized middleware, and centralized exception formatting ([official CLI entry point](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/index.ts)).
- The package uses `@opentui/core`, `@opentui/solid`, `@opentui/keymap`, SolidJS, `@clack/prompts`, and `opentui-spinner`; this separates the full-screen TUI from lightweight command-line presentation ([official package manifest](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/package.json)). OpenTUI itself describes a native Zig core with Solid and React reconcilers plus a shared keybinding engine, and states that it powers OpenCode in production ([official OpenTUI repository](https://github.com/anomalyco/opentui)).
- The `run` implementation consumes one event stream and projects it into either raw JSON events or presentation helpers. This event-to-renderer boundary is the most reusable architectural idea for AgentComms ([official `run.ts`](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/cli/cmd/run.ts)).

## OpenClaw

### Output contract

- OpenClaw explicitly documents three terminal rules: ANSI styling and progress appear only in TTY sessions; OSC-8 links become clickable where supported and fall back to plain URLs; and `--json` reserves stdout for one JSON document while suppressing styling/progress and leaving warnings/diagnostics on stderr ([official CLI reference](https://github.com/openclaw/openclaw/blob/main/docs/cli/index.md)).
- It scopes `--json` pragmatically: bounded reporting commands should support it, while interactive wizards, long-running streams/servers, shell integration, and pure side-effect commands may omit it when no meaningful report exists ([official CLI reference](https://github.com/openclaw/openclaw/blob/main/docs/cli/index.md)).
- Gateway query commands consistently offer human-readable colored output, `--json`, `--no-color`, connection/auth parameters, timeouts, and an optional wait-for-final behavior ([official gateway CLI documentation](https://github.com/openclaw/openclaw/blob/main/docs/cli/gateway.md)).

### Color, progress, tables, and detail

- OpenClaw publishes a semantic “lobster” palette: accent variants for hierarchy, info for values, green for success, amber for warnings, red for errors, and muted/dim tokens for secondary content. This creates brand identity without using color as the only signal ([official CLI reference](https://github.com/openclaw/openclaw/blob/main/docs/cli/index.md)).
- Long-running commands show progress, including OSC 9;4 terminal progress where supported. Since progress is TTY-only and suppressed in JSON mode, animation does not become part of the data stream ([official CLI reference](https://github.com/openclaw/openclaw/blob/main/docs/cli/index.md)).
- Model discovery demonstrates information layering: human mode uses a table with compact capability columns such as `Input` and `Ctx`; JSON preserves richer fields and distinguishes native context windows from effective runtime caps. Status mode summarizes defaults, fallbacks, authentication health, cooldowns, quotas, and actionable recovery information ([official models CLI documentation](https://github.com/openclaw/openclaw/blob/main/docs/cli/models.md)).
- Diagnostic depth is controlled rather than dumped: status is concise by default, additional severities can be requested, and JSON separates concepts such as auth status and runtime status instead of flattening them into opaque text ([official models CLI documentation](https://github.com/openclaw/openclaw/blob/main/docs/cli/models.md)).

### Errors and configuration UX

- Configuration has focused `get`, `set`, `patch`, `unset`, `file`, `schema`, and `validate` operations. Machine-mode errors can be represented as JSON while still returning exit status 1; human diagnostics remain on stderr ([official config CLI documentation](https://github.com/openclaw/openclaw/blob/main/docs/cli/config.md)).
- Config writes validate model references before mutation, reject unsafe replacements unless an explicit `--replace` is supplied, and support strict JSON parsing. These patterns combine useful errors, atomicity, and explicit consent for destructive changes ([official config CLI documentation](https://github.com/openclaw/openclaw/blob/main/docs/cli/config.md)).
- Global configuration/discoverability includes `--profile` isolation, `--dev`, `--log-level`, `--no-color`, conventional help/version behavior, and `NO_COLOR` support ([official CLI reference](https://github.com/openclaw/openclaw/blob/main/docs/cli/index.md)).

### Architecture and libraries

- OpenClaw's official manifest uses Commander for command routing, Chalk for terminal styling, Clack Core/Prompts for interactive workflows, `@earendil-works/pi-tui` for terminal UI, `pretty-ms` for duration formatting, and `string-width` for display-width correctness ([official package manifest](https://github.com/openclaw/openclaw/blob/main/package.json)).
- The documented output rules imply a centralized runtime/presentation policy rather than per-command ad hoc checks: TTY, color, JSON, progress, hyperlinks, stdout, and stderr are treated as cross-cutting CLI concerns ([official CLI reference](https://github.com/openclaw/openclaw/blob/main/docs/cli/index.md)).

## Comparison

| Concern | OpenCode | OpenClaw | Lesson for AgentComms |
|---|---|---|---|
| Default experience | Full TUI with no args; formatted one-shot `run` | Rich command hierarchy plus interactive wizards/TUI surfaces | Keep a polished non-interactive CLI first; add interactive surfaces where they materially improve workflows |
| Structured output | `--format json`; streaming run uses JSON events | `--json`; bounded commands emit one clean document | Define both a bounded JSON document contract and JSONL event contract |
| Human output | Agent/model headings, glyph-based tool summaries, optional blocks | Semantic branded palette, tables, summaries, progress | Build shared summary, table, detail, status, and error components |
| Progress | TUI and spinner dependencies; streamed event rendering | Explicit TTY-only progress and OSC 9;4 support | Centralize progress and disable it automatically for pipes/JSON |
| Diagnostics | Logs on stderr; log level; formatted errors | Diagnostics/warnings on stderr; severity/detail controls | Reserve stdout for the requested result |
| Configuration | Schema-backed TUI config, themes, keybindings, environment variables | Profiles, `NO_COLOR`, schema/validate commands, guarded writes | Support global output preferences but let flags override config |
| Stack | Yargs + OpenTUI/Solid + small CLI UI helpers | Commander + Chalk + Clack + pi-tui | Choose libraries after defining an output abstraction; do not couple command logic to a renderer |

## Recommended direction for AgentComms

### 1. Establish an output contract before visual polish

Every command handler should return a typed semantic result or throw a typed CLI error. It should not stringify arbitrary backend responses. A single presenter owns serialization and terminal rendering.

Suggested modes:

- `human` (default): summary-first, color only in a TTY.
- `json`: one valid JSON document for bounded commands; no progress or styling on stdout.
- `jsonl`: one versioned event per line for streaming commands.
- `plain`: stable, uncolored human-readable text for logs and basic piping.

Consider `--output human|json|jsonl|plain` as the canonical flag, with `--json` retained as a convenient alias. Avoid calling JSON “raw”: it is a supported interface and needs compatibility discipline.

### 2. Implement a small design system

Create semantic tokens (`accent`, `muted`, `info`, `success`, `warning`, `danger`) and reusable primitives:

- heading and key/value summary;
- status line with both glyph and label;
- width-aware table with sensible column priority;
- detail block for verbose payloads;
- spinner/progress lifecycle;
- actionable error with summary, cause, and next step;
- OSC-8 link with plain-URL fallback.

Use Unicode glyphs only when terminal capability allows and always pair them with words. Respect `NO_COLOR`, `TERM=dumb`, non-TTY output, and an explicit `--no-color` flag.

### 3. Apply progressive disclosure

The default output should answer: what happened, whether it succeeded, and what the user should do next. Put IDs, paths, duration, and important counts in a compact summary. Add:

- `--verbose` for operational metadata;
- `--quiet` for only the primary result;
- `--details` for full nested fields where useful;
- `--debug` or existing log-level controls for diagnostic traces.

Never print an entire nested response simply because a command lacks a bespoke view. Unknown result shapes should fall back to a readable tree/detail renderer, not minified JSON.

### 4. Make failure behavior part of the design

Use stable exit codes and typed error categories (usage, auth, unavailable, validation, conflict, network, internal). Human errors should include a concise remedy. JSON errors should have a stable envelope such as:

```json
{
  "ok": false,
  "error": {
    "code": "AUTH_REQUIRED",
    "message": "Authentication is required",
    "hint": "Run agentcomms auth login"
  }
}
```

In all modes, return non-zero on failure. Debug traces stay on stderr and are opt-in.

### 5. Roll out by command family

1. Inventory every command, current stdout/stderr behavior, response shape, and exit code.
2. Add the output context and presenter abstraction with golden/snapshot tests.
3. Convert the command currently exposing raw JSON; use it as the reference implementation.
4. Convert read/list/status commands to summary/table/detail plus JSON.
5. Convert long-running commands to TTY progress plus JSONL events.
6. Convert configuration and mutation commands, adding confirmation/dry-run where destructive.
7. Improve help, examples, completion, aliases, and “next command” hints.
8. Consider a full TUI only after the command/result model is stable; the same semantic events should feed both CLI and TUI renderers.

### 6. Verification gates

- Snapshot human output at narrow, normal, and wide terminal widths.
- Assert zero ANSI/control bytes in redirected, `plain`, and JSON output.
- Pipe every JSON command through `jq` in CI and verify stderr cannot contaminate stdout.
- Test `NO_COLOR`, `TERM=dumb`, non-TTY stdin/stdout, interrupted spinners, and Windows terminals.
- Contract-test JSON/JSONL schemas and version streaming event envelopes.
- Verify errors produce useful messages and non-zero exit codes without stack traces by default.

## Important caution

Rich CLI libraries do not create good UX by themselves. OpenCode's strongest architectural choice is translating semantic events into different views; OpenClaw's strongest contract is strict separation of human presentation, machine data, and diagnostics. AgentComms should adopt those boundaries first, then layer in its own visual identity.
