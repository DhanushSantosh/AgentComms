# Lower landing page: design QA

Date: 2026-10-01 · Owner of record: claude-main (task `landing-approved-20261001`,
handed over from codex-main at the owner's request)

**Status: owner-reviewed and committed; integration pending in PR #73.**
Linux landing visual baselines are refreshed. The remaining Windows baseline
and media follow-ups are listed below; CI must pass before merging into `dev`.

This replaces the earlier QA log for this task. That log tracked several
superseded designs (an image-matched compact version, a "restore content"
version, a density-tightened version), and its first line read "final result:
blocked". The history is in the AGC record (task `landing-approved-20261001`,
messages #391–#406) rather than here.

## What was wrong with the version handed over

An audit of the live page before any edit found the problem was structural,
not spacing:

- Seven blocks below the thesis used five different layouts (card grid,
  illustrated rows, bare text, numbered list, comparison) and two labelling
  schemes ("01 / OWNERSHIP" vs "CONTROL / HUMAN AUTHORITY").
- Governance was told three times (Human control, "Inspect the evidence",
  Trust) and the handoff story three times (Reach the team, Every handoff
  leaves a trail, Delivery isn't acknowledgement).
- Two blocks were a single paragraph next to illustrated rows.
- Its CSS reused the original design's section classes (`.control`,
  `.modes`, `.mode`, `.trust`) and spent its effort overriding legacy rules,
  including duplicated selectors and a `.product-details.product-details`
  specificity hack.

## Structure (owner decision: merge true duplicates, keep every topic)

Six numbered sections after the unchanged hero and thesis:

| # | Label | Heading | Visual | Absorbs |
|---|---|---|---|---|
| 01 | Ownership | Own the work. | illustration | — |
| 02 | Coordination | Reach the team. | illustration | — |
| 03 | Handoff | Every handoff leaves a trail. | 4-step trail | lifecycle |
| 04 | Governance | Human control when it matters. | illustration + facts | "Inspect the evidence" |
| 05 | Trust | Trust is not a badge. It is the shape of every write. | 4-step write path | — |
| 06 | Deployment | One model. Two ways to run. | comparison table | — |

Original full footer kept; final CTA and navbar scroll loader stay removed.

## System

- One component, `src/components/ProductSections.tsx`; one CSS block, all
  namespaced `.ps-*` so no legacy rule can reach it.
- One anatomy for every section: label, heading, body, link, one visual.
- Three reused visual primitives: illustration (`.ps-art`), connected step
  list (`.ps-steps`), aligned fact rows (`.ps-facts`, and `.ps-compare` as a
  real table). Illustrated sections alternate sides; deployment is full width
  with a two-column header.
- Typography: 60px display headings at 1440px, 12px mono labels, 44ch body
  measure; same in every section.
- Width: one frame for every section, the same as the control-room (WASM)
  window above: the content width inside `--gutter`, capped at 84rem. Text
  column (34rem) on one edge, visual column (36rem) on the other, mirrored
  in 02 and 04; the remaining space is the gap. Text and visual are centred
  on each other. Illustrations and step lists fill the 576px visual column.
- Section 06 does not split left/right: a centred introduction, then the
  comparison table across the full frame. On phones the table is replaced
  by one block per mode (its intro plus its four details), rendered from
  the same data; only one of the two is ever displayed.
- Reveal: text and visual are observed separately and reveal when ~10% into
  the viewport (`revealRootMargin`); the text staggers label, heading, body,
  facts, link (110ms apart) like the hero and footer, and the visual follows
  two beats after the label. Measured: every part triggers at 84-90% of the
  viewport height, desktop and phone.

### Spacing (owner: "the spacing on section 2 is good, make it consistent")

Every section has section 02's clearance between its borders and its
nearest content: `--ps-space`, 128px at 1440px and above, 56px on phones.

Two things made this hold at every width rather than only in the padding:

1. Side by side, an illustration's box (taller than the text) used to set the
   section height, which put the text ~40px further from the borders in
   illustrated sections than content sat elsewhere. Illustrations are now
   size-contained on desktop: the text sets the height and the art centres on
   it, appearing exactly where it did before.
2. The illustration files carry uneven empty bands (≈31% above and below
   "coordination", 12–16% on the others). On phones, where art stacks under
   the text, those bands were visible gaps of different heights. Each frame
   is cropped to its drawing (measured from the image pixels, backed off one
   point so no edge is clipped).

Measured clearance (top/bottom px, identical across all six sections):

| Width | Clearance |
|---|---|
| 390 | 57 / 56 |
| 768 | 75 / 74 |
| 1280 | 124 / 123 |
| 1440 | 129 / 128 |
| 1920 | 129 / 128 |

## Product claims (checked against code, asserted in tests)

- Overlapping claims are refused unless a shared-write approval allows it;
  nothing is reassigned automatically (`transitions.go`, `doctor.go`).
- Messages and requests are durable without a runtime; automatic delivery and
  claims need one.
- Delivery is not acknowledgement; completion is a reported result, not proof.
- Team mode is a deliberate deployment, not a migration of a personal project
  (no such command exists).
- No simulated output is presented as recorded.

## Accessibility

- Headings, labels and mode titles contain real spaces between their visual
  lines (previously "handoffleaves" in the accessible text).
- The comparison is a `<table>` with explicit roles, so it stays a table when
  phones reflow it to block display; the corner header is visually hidden,
  not `display:none`, so it remains in the accessibility tree.
- Reveal motion only applies once JS marks the page `motion-ready`, so no-JS
  readers see everything; the reduced-motion override now matches the
  hidden-state rule's specificity and actually removes the slide.
- Nav fixed: "Control room" pointed at the governance section; it now goes to
  the live TUI (`#live-tui`). All nav anchors target real sections.

## Verification

- `npm run check` (TypeScript + production build): pass.
- `tests/landing.spec.ts`, desktop and mobile Chromium, after a fresh
  production build: 55 passed, 3 intentional device-specific skips. New tests cover section order, a shared
  anatomy and rhythm (including the visible clearance against section 02),
  reveal, illustration size caps, nav anchors, documentation links, product
  claims, the comparison table, no-JS and a 320px reduced-motion viewport.
- Headless Chromium at 390, 768 and 1440px: every section reveals, no
  horizontal overflow.
- `tests/visual.spec.ts`: the hero baseline still matches (hero unchanged);
  the lower-page baselines fail because they show the previous design.

## Open

- [x] Owner review of the rendered page (spacing, frame width, section 06,
      reveal motion approved over several rounds on 2026-10-01).
- [x] Lower-page linux visual baselines refreshed.
- [x] Push `feat/landing-product-sections` and open PR #73 against `dev`.
- [ ] Merge PR #73 after the final CI checks pass.
- [ ] The win32 baselines for `landing-protocol` and `landing-control-room`
      can only be regenerated on Windows.
- [ ] The docs captures (docs/img, sites/docs/public) predate the TUI's
      singular/plural fix and still read "1 open tasks"; refresh per
      docs/img/README.md.
- Resolved on this branch: the live control room's rows were centred by
  the hero's inherited text-align (fixed, guarded by the launch test); the
  TUI overview's plural counts; dead code for removed sections; production
  `npm audit` reports 0 vulnerabilities.
