# Landing Site Illustration Consistency — Design

**Status:** Approved by user 2026-09-20, ready for implementation planning.

## Context

A fresh section-by-section visual audit of `sites/landing`'s homepage (this session, 2026-09-19/20) found and fixed three concrete defects: a layout collision in Direct Agent Relay, a broken color-differentiation selector in Handoff Evidence, and a missing illustration entirely in the "Human Control" section (now `.control-signal`). Fixing those surfaced a broader pattern: the page's six custom-UI sections (`LiveControlRoom`/hero, `CollisionLab`, `DemoReel`/Handoff Evidence, `LifecycleOrbit`, the inline Relay markup, `ModeBridge`, `.trust-chain`, `.control-signal`) each invented their own visual grammar rather than sharing one, so the page reads as several different demos stapled together instead of one consistent product.

Real research into Anthropic's own site (anthropic.com, claude.com/product/claude-code, claude.com/product/overview — screenshots taken live this session, not recalled from memory) found the opposite pattern: one consistent illustration frame (the browser-chrome-style window on claude.com) reused everywhere with varying content, restrained to one accent color per moment, plus one recurring abstract connective motif (the scribble/line texture used as a placeholder) rather than a different diagram style per section.

**Hard constraint carried over from prior work on this site (not renegotiated here):** the TUI-exact palette (`--ink #000000`, `--panel #0d2024`, `--cyan #56d6c9`, `--amber #e8b85c`, `--lilac #b9a7e8`, `--coral #f07167`) stays exactly as-is. This design changes only which components get the frame, not any color value.

## Current state (audited 2026-09-20)

Three sections already use the correct shared "flagship panel" treatment (`background: var(--panel)`, `border: 1px solid var(--line-dark)`, `box-shadow: 1.2rem 1.2rem 0 var(--cyan)`, cyan corner-tick pseudo-elements via the shared selector list in `globals.css`, plus a `<span>LABEL</span><span>META</span>` mono header-bar):

- `.demo-reel.evidence-film` (Handoff Evidence) — `.evidence-film-label`
- `.trust-chain` (Provable Trust) — `.trust-chain-head`
- `.control-signal` (Human Control, added this session) — `.control-signal-head`

Three do not:

- **`LifecycleOrbit`** (`src/components/LifecycleOrbit.tsx` + its own `LifecycleOrbit.css`) — `.lifecycle-orbit` has `background: var(--ink)` (same as the section background — visually undifferentiated), no border, no shadow, no corner-tick, no header bar. It's a centered circular composition, not currently framed as a panel at all.
- **Relay** (`page.tsx` inline, styled in `globals.css`) — `.relay-sequence::before` is a rotated (`rotate(-2deg)`) hairline outline, not the shared panel/shadow/corner-tick treatment. No header bar.
- **`ModeBridge`** (`src/components/ModeBridge.tsx`, styled in `globals.css` as `.mode-bridge.continuity-map`) — `background: transparent; border: 0; border-bottom: 1px solid var(--line-dark)`. Has a centered `.continuity-heading` ("LOCAL → SHARED"), not the shared left/right header-bar format. No shadow, no corner-tick.

One drifts partially: **`CollisionLab`** (`src/components/CollisionLab.tsx` + its own `CollisionLab.css`) already has the panel/shadow/corner-tick treatment (its selectors are in the shared `globals.css` list), but its own `.scenario-header` (one per side, `<strong>` + `<small>`) uses different typography than the shared header-bar convention.

Separately (noted, not in scope for this design — flagged as a follow-up): both `CollisionLab.css`-era and `ModeBridge`-era rewrites left dead CSS behind in `globals.css` from earlier versions of those components (`.collision-zone`/`.code-track--reviewer`/`.governed-result` around lines 387-413; `.mode-bridge-row`/`.mode-bridge-node`/`.mode-bridge-caption` etc. around lines 899-913 and 1276-1280) — none of these class names appear in the current component JSX. Real dead code, unrelated to illustration consistency; worth a separate cleanup pass.

Four "delivered ≠ acknowledged"-style connector/gap moments currently render four different ways:

- Handoff Evidence: `.evidence-film-gap` — solid `background: var(--text)` callout box between cards.
- Relay: `.relay-gap` — solid `background: var(--text)` callout card, absolutely positioned.
- Lifecycle orbit: `.semantic-gap` — plain stacked text, no connecting line/box at all, centered inside the ring.
- Human Control: `.control-signal-arrow` — a horizontal-rule-flanked text row (`::before`/`::after` 1px lines).

## Design

### Part 1: The unified frame

Extend the existing shared selector list in `globals.css` (currently `.control-frame, .demo-reel, .collision-lab, .trust-chain, .control-signal`) to also cover `.lifecycle-orbit-panel` (see below — a new wrapping element, not `.lifecycle-orbit` itself), `.relay-sequence`, and `.mode-bridge`. Each gets:

- `background: var(--panel); border: 1px solid var(--line-dark); box-shadow: 1.2rem 1.2rem 0 var(--cyan); position: relative;`
- The shared corner-tick `::before`/`::after` pseudo-elements (cyan, 0.9rem, top-left/bottom-right).
- A header bar matching the established convention exactly: `display: flex; justify-content: space-between; font-family: var(--mono); font-size: 0.46rem; letter-spacing: 0.08em; color: var(--steel-light);` with two `<span>` children (label left, meta right).

Per-component application:

- **`LifecycleOrbit`**: wrap the existing `.lifecycle-orbit` circle in a new outer `.lifecycle-orbit-panel` element that carries the panel/shadow/corner-tick treatment and a new header bar (`<span>LIFECYCLE PROTOCOL</span><span>REQUESTED → COMPLETED</span>`, matching the section's own "05 / LIFECYCLE PROTOCOL" eyebrow). The circle itself, its aspect-ratio math, and its centering are untouched — it becomes the content inside the new frame, not replaced.
- **Relay**: replace `.relay-sequence::before`'s rotated hairline outline with the shared panel/shadow/corner-tick treatment (no rotation on the outer frame). Add a header bar above the existing party/message/evidence/claim/result composition (`<span>DIRECT AGENT RELAY</span><span>LIVE · SIMULATED</span>`). The individual cards inside (DEVELOPER, TESTER, message, claim, result) keep their existing slight individual rotations — only the outer frame stops rotating.
- **`ModeBridge`**: add the panel/shadow/corner-tick treatment to `.mode-bridge.continuity-map` (replacing its current transparent/border-bottom-only treatment) and add a header bar above the existing `.continuity-heading` (`<span>LOCAL TO SHARED</span><span>ONE MACHINE → TWO MACHINES</span>`), keeping `.continuity-heading`'s own centered "LOCAL → SHARED" arrow row as content below it.
- **`CollisionLab`**: no structural change. Align `.scenario-header strong`/`.scenario-header small`'s font-size and letter-spacing with the shared header-bar convention's values, so both sides read as the same typographic family as every other section's header bar even though the two-sided content itself is unchanged.

### Part 2: One shared gap/connector motif

New shared class (name: `.signal-gap`, added to `globals.css` once) replacing the three divergent implementations:

- A thin dashed horizontal line (`border-top: 1px dashed color-mix(in srgb, var(--text) 30%, transparent)`) spanning the available width, with a centered pill label on top (`background: var(--ink-raised); border: 1px solid var(--line-dark); padding: 0.3rem 0.6rem; font-family: var(--mono); font-size: 0.44rem; letter-spacing: 0.06em;`), matching the visual weight of `.control-signal-arrow` (already built this session) rather than the heavier solid-block treatment `.evidence-film-gap`/`.relay-gap` currently use.

Applied to all four gap moments, replacing their current one-off treatments:

- `.evidence-film-gap` (Handoff Evidence) — same label content ("WAITING FOR ACKNOWLEDGEMENT" / "DELIVERED ≠ ACKNOWLEDGED"), new shared visual treatment.
- `.relay-gap` (Relay) — same label content, new shared visual treatment.
- `.semantic-gap` (Lifecycle orbit) — currently has no connecting line at all, just plain stacked text. Gains one short dashed rule (same `border-top: 1px dashed ...` treatment, sized to the pill's own width) immediately above the pill and another immediately below it, in place of the current bare text stack. It does not span the full row the way the other three do, since it sits centered inside a circle rather than between two block-level siblings.
- `.control-signal-arrow` (Human Control) — already matches this shape closely (built this session); reconciled to use the exact same shared class rather than its own near-duplicate rules.

## Out of scope

- Approach 3 from the original proposal (replacing the orbit chart and Relay's card composition with realistic recreated-UI screens, matching Claude Code's product page most literally) — not part of this pass. This design only unifies the *frame* and the *gap motif*; it does not redesign any section's core content/diagram.
- The dead CSS noted above (`.collision-zone`/`.code-track--*`/`.governed-result`, `.mode-bridge-row`/`.mode-bridge-node`/etc.) — real, confirmed, but unrelated to this design; flagged separately.
- Any change to the TUI-exact color palette itself.
- `LiveControlRoom` (hero) — already the real embedded product UI, not a mockup; not part of this consistency pass.

## Testing / validation

- `npx tsc --noEmit` after each component change.
- Full `npm run build` (static export) before considering the pass done.
- Visual check via the Browser pane for all six touched/verified sections (Lifecycle, Relay, ModeBridge, CollisionLab, plus re-check Handoff Evidence and Human Control's gap motif after the shared-class swap), at both desktop width and the `mobile` preset (375px), since three of the six components (`LifecycleOrbit`, `CollisionLab`, `ModeBridge`) have their own `@media` breakpoints that must keep working after the wrapping-panel change.
- No new console errors.
