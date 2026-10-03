import { expect, test } from "@playwright/test";

// The lower page is six numbered sections, one topic each, in this order.
// Governance and the handoff story used to appear three times each; the
// duplicates were merged at the owner's direction, so every topic below is
// asserted exactly once.
const productSections = [
  { id: "ownership", label: "01 Ownership", heading: "Own the work.", path: "/guide/work/", art: "ownership" },
  { id: "coordination", label: "02 Coordination", heading: "Reach the team.", path: "/guide/communication/", art: "coordination" },
  { id: "handoff", label: "03 Handoff", heading: "Every handoff leaves a trail.", path: "/agents/invocations/", art: null },
  { id: "control", label: "04 Governance", heading: "Human control when it matters.", path: "/guide/governance/", art: "governance" },
  { id: "trust", label: "05 Trust", heading: "Trust is not a badge. It is the shape of every write.", path: "/security/integrity/", art: null },
  { id: "deployment", label: "06 Deployment", heading: "One model. Two ways to run.", path: "/start/modes/", art: null }
] as const;

test("presents the product thesis and six product sections in order", async ({ page }) => {
  await page.goto("/");

  await expect(page.getByRole("heading", { level: 1, name: /Let agents work at once/ })).toBeVisible();
  await expect(page.getByText("Keep the project in one piece.")).toBeVisible();
  await expect(page.locator(".hero").getByRole("link", { name: /Install Agent Comms/ })).toHaveAttribute("href", "/download");
  await expect(page.getByRole("link", { name: "Docs", exact: true }).first()).toHaveAttribute("href", "https://agentcomms-docs.vercel.app");
  await expect(page.getByRole("region", { name: "Product thesis" })).toContainText("Chat is where agents talk.");

  const ids = await page.locator("[data-product-section]").evaluateAll((sections) => sections.map((section) => section.id));
  expect(ids).toEqual(productSections.map((section) => section.id));
  // The navbar scroll loader and the final CTA stay removed.
  expect(await page.locator(".site-header").evaluate((header) => getComputedStyle(header, "::after").content)).toBe("none");
  expect(await page.locator(".site-header").evaluate((header) => getComputedStyle(header, "::before").content)).toBe("none");
  await expect(page.locator(".cta-banner")).toHaveCount(0);
});

// One anatomy, one rhythm: every section has the same parts and the same
// padding, heading size and label style. This is what failed before -- seven
// blocks in five layouts -- so a section that drifts should fail here.
test("all six sections share one anatomy and one rhythm", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/");
  const measurements = await page.locator("[data-product-section]").evaluateAll((sections) => sections.map((section) => {
    const style = getComputedStyle(section);
    return {
      id: section.id,
      parts: [".ps-label", "h2", ".ps-body", ".ps-link", ".ps-visual"].map((selector) => section.querySelectorAll(selector).length),
      paddingTop: style.paddingTop,
      paddingBottom: style.paddingBottom,
      headingSize: getComputedStyle(section.querySelector("h2")!).fontSize,
      labelSize: getComputedStyle(section.querySelector(".ps-label")!).fontSize
    };
  }));
  for (const section of measurements) {
    expect(section.parts, `${section.id} anatomy`).toEqual([1, 1, 1, 1, 1]);
  }
  for (const key of ["paddingTop", "paddingBottom", "headingSize", "labelSize"] as const) {
    expect(new Set(measurements.map((section) => section[key])).size, `${key} must be uniform`).toBe(1);
  }

  // Uniform padding is not enough: it was uniform when the spacing still
  // looked inconsistent, because an illustration taller than its text pushed
  // that text ~40px further from the borders than content sat elsewhere. What
  // a reader sees is the gap between each border and the nearest content, so
  // that is what must match -- section 02's, which the owner chose.
  const clearances = await page.locator("[data-product-section]").evaluateAll((sections) => sections.map((section) => {
    const box = section.getBoundingClientRect();
    const copy = section.querySelector(".ps-copy")!.getBoundingClientRect();
    const visual = section.querySelector(".ps-visual")!;
    // Side by side, an illustration is size-contained and never sets the
    // section's edge, so the text does. Stacked on phones, the illustration
    // is ordinary content and counts like any other visual.
    const contained = getComputedStyle(visual).contain.includes("size");
    const edges = contained ? [copy] : [copy, visual.getBoundingClientRect()];
    // Text and visual are centred on each other (the owner's choice), so
    // where a step list is taller than its text the list sets the edge and
    // the text sits slightly lower; the clearance is to the nearest content.
    return {
      id: section.id,
      top: Math.min(...edges.map((edge) => edge.top)) - box.top,
      bottom: box.bottom - Math.max(...edges.map((edge) => edge.bottom))
    };
  }));
  const reference = clearances.find((section) => section.id === "coordination")!;
  for (const section of clearances) {
    expect(Math.abs(section.top - reference.top), `${section.id} top clearance`).toBeLessThanOrEqual(2);
    expect(Math.abs(section.bottom - reference.bottom), `${section.id} bottom clearance`).toBeLessThanOrEqual(2);
  }
});

test("every section reveals and its illustration stays within the size cap", async ({ page }) => {
  await page.goto("/");
  for (const { id, art } of productSections) {
    const section = page.locator(`#${id}`);
    // Text and visual reveal separately, each as it comes into view.
    for (const part of ["copy", "visual"]) {
      const element = section.locator(`.ps-${part}`);
      await expect(element).toHaveAttribute("data-reveal", `${id}-${part}`);
      await element.scrollIntoViewIfNeeded();
      await expect(element).toHaveClass(/is-revealed/);
    }
    await expect(section.locator(".ps-link")).toHaveCSS("opacity", "1");
    await expect(section.locator(".ps-visual")).toHaveCSS("opacity", "1");
    if (art) {
      const image = section.locator(".ps-art img");
      expect(await image.evaluate((el) => el.getBoundingClientRect().width)).toBeLessThanOrEqual(576);
    }
  }
});

test("mobile navigation opens, closes, and preserves keyboard semantics", async ({ page, isMobile }) => {
  test.skip(!isMobile, "Mobile-only navigation behavior");
  await page.goto("/");

  const toggle = page.getByRole("button", { name: "Open navigation" });
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByRole("navigation", { name: "Primary navigation" })).toBeVisible();
  await page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: "Governance", exact: true }).click();
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
});

test("navigation anchors land on real sections, and Control room on the live TUI", async ({ page }) => {
  await page.goto("/");
  const hrefs = await page.locator("[data-site-navigation] a[href^='/#']").evaluateAll((links) => links.map((link) => link.getAttribute("href")));
  expect(hrefs.length).toBeGreaterThan(0);
  for (const href of hrefs) {
    await expect(page.locator(href!.slice(1)), `${href} must exist`).toHaveCount(1);
  }
  // includeHidden: on phones the links sit inside the closed menu.
  await expect(page.locator("[data-site-navigation]").getByRole("link", { name: "Control room", includeHidden: true }).first()).toHaveAttribute("href", "/#live-tui");
});

test("supports a keyboard skip path", async ({ page }) => {
  await page.goto("/");
  await page.keyboard.press("Tab");
  const skipLink = page.getByRole("link", { name: "Skip to content" });
  await expect(skipLink).toBeFocused();
  await skipLink.press("Enter");
  await expect(page.locator("#main-content")).toBeFocused();
});

test("sections link to real documentation and use the approved illustrations", async ({ page }) => {
  await page.goto("/");
  for (const { id, label, heading, path, art } of productSections) {
    const section = page.locator(`#${id}`);
    await section.scrollIntoViewIfNeeded();
    await expect(section.locator(".ps-label")).toHaveText(label);
    await expect(section.getByRole("heading", { level: 2 })).toHaveText(heading);
    await expect(section.locator(".ps-link")).toHaveAttribute("href", `https://agentcomms-docs.vercel.app${path}`);
    if (art) {
      await expect.poll(() => section.locator("img").evaluate((img) => (img as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
      await expect(section.locator("img")).toHaveAttribute("src", `/illustrations/${art}.webp`);
    }
    expect(await section.evaluate((el) => el.getBoundingClientRect().right)).toBeLessThanOrEqual(page.viewportSize()!.width + 1);
  }
});

// Claims the product cannot back must not come back: no simulated output
// presented as recorded, no automatic reassignment of a refused claimant,
// and no seamless personal-to-team migration. The accurate versions are
// asserted in their place.
test("product claims stay within what the product does", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("[data-demo-reel], .lifecycle-orbit, [data-relay-sequence]")).toHaveCount(0);
  await expect(page.getByText(/FOUR RECORDED FACTS|LIVE · SIMULATED|24 \/ 24 auth tests pass|ALTERNATE TASK ASSIGNED|without a mode switch/)).toHaveCount(0);
  await expect(page.locator("#ownership")).toContainText("refused unless a shared-write approval allows it");
  await expect(page.locator("#coordination")).toContainText("reached while its runtime is offline");
  await expect(page.locator("#handoff")).toContainText("Delivery is not acknowledgement");
  await expect(page.locator("#deployment")).toContainText("not an automatic migration of a personal project");
});

test("the deployment comparison is a table on wide screens and one block per mode on phones", async ({ page }) => {
  await page.goto("/");
  const table = page.locator("#deployment table");
  const modes = page.locator("#deployment .ps-mode");
  const narrow = (page.viewportSize()?.width ?? 1440) <= 768;
  if (!narrow) {
    await expect(table).toBeVisible();
    await expect(modes.first()).toBeHidden();
    await expect(table.getByRole("columnheader")).toHaveCount(3);
    await expect(table.getByRole("rowheader")).toHaveText(["Best for", "Authority", "Setup", "Reads"]);
    await expect(table.locator("td")).toHaveCount(8);
    await expect(table.getByRole("cell", { name: "Service, PostgreSQL, authentication and backups" })).toBeVisible();
  } else {
    await expect(table).toBeHidden();
    await expect(modes).toHaveCount(2);
    for (const [index, name] of ["Your project. Your local agents.", "Your team. One shared authority."].entries()) {
      const mode = modes.nth(index);
      await expect(mode.getByRole("heading", { level: 3 })).toHaveText(name);
      await expect(mode.locator("dt")).toHaveText(["Best for", "Authority", "Setup", "Reads"]);
    }
    await expect(modes.nth(1)).toContainText("Service, PostgreSQL, authentication and backups");
  }
});

test("product sections remain readable without JavaScript", async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  await page.goto("http://127.0.0.1:4333/");
  for (const { heading } of productSections) {
    await expect(page.getByRole("heading", { level: 2, name: heading, exact: true })).toBeVisible();
  }
  await expect(page.locator(".cta-banner")).toHaveCount(0);
  await expect(page.getByRole("contentinfo")).toBeVisible();
  await context.close();
});

test("lower page works with reduced motion and a narrow viewport", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.setViewportSize({ width: 320, height: 740 });
  await page.goto("/");
  for (const { id } of productSections) {
    const section = page.locator(`#${id}`);
    await section.scrollIntoViewIfNeeded();
    expect(await section.evaluate((el) => el.getBoundingClientRect().right)).toBeLessThanOrEqual(321);
    await expect(section.locator(".ps-link")).toBeVisible();
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
});

// Small screens receive an authentic recording rather than a drifting
// hand-authored TUI recreation. Nothing plays or downloads automatically.
test("mobile control room uses the current recording without autoplay", async ({ page, isMobile }) => {
  test.skip(!isMobile, "mobile recording; desktop retains the real interactive TUI");
  await page.goto("/");
  const video = page.locator("[data-tui-recording] video");
  await expect(video).toBeVisible();
  await expect(video).toHaveAttribute("poster", "/media/tui-overview.png");
  await expect(video).toHaveAttribute("preload", "none");
  await expect(video).toHaveAttribute("controls", "");
  expect(await video.getAttribute("autoplay")).toBeNull();
  await expect(video.locator("source")).toHaveAttribute("src", "/media/tui-demo.mp4");
  const box = await video.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.x + box!.width).toBeLessThanOrEqual(page.viewportSize()!.width + 1);
  await expect(page.getByText("RECORDED FROM THE CURRENT TUI")).toBeVisible();
});

// This drives the *real*, WASM-compiled product TUI -- LiveControlRoom.tsx
// lazy-loads cmd/agent-comms-tui-wasm + xterm.js and mounts it live, in
// place of the static poster above, on desktop/tablet-landscape viewports
// wide enough for the real product's layout. The keystroke sequence below
// is not guessed: it is the
// exact real key binding internal/tui/model.go's key handling uses to
// reach the seeded Approvals row --
//   - "]" -> Model.moveHubView(1): cycles the *current hub's* own tabs.
//     The default view on launch is "Overview", whose hub ("Command") has
//     Views: ["Overview", "My work", "Blockers", "Approvals"] (see
//     navigationHubs in model.go), so three presses of "]" lands on
//     Approvals. There is no "Tab" binding for this at all in the real
//     keymap -- "tab"/"shift+tab" are reserved for a *form's* own field
//     navigation (model.go's updateForm), a different mode entirely.
//   - "enter" -> focuses the row list (m.rowFocus = true), selecting the
//     one seeded approval row (seed.go's pendingApprovalID,
//     "approval-orchestrator-reviewer", left PENDING deliberately so a live
//     visitor has a real decision to make).
// This exact sequence (three "]" then "enter") is the same one
// internal/tui/approvals_test.go's enterApprovalsView helper uses to reach
// this view in Go's own test suite -- not improvised here.
//
// From there, this test rejects the pending approval (RowAction Key "x",
// approvals.go's appReject) rather than approving it: approving a
// HUMAN-tier approval opens a masked-passphrase form (approveActionFor),
// while reject only needs a single "y" to confirm (rowlist.go's
// updateConfirm) -- both are real, terminal state transitions the seed
// deliberately leaves available, but reject is the smaller, less brittle
// keystroke sequence to drive through a real xterm.js terminal while still
// proving the exact thing the plan's Global Constraints require: driving
// the seeded approval to completion through the live TUI must actually
// change what renders.
test("launches the real TUI in the control room and can act on the seeded approval", async ({ page, isMobile }) => {
  // The real TUI renders into a fixed character grid (xterm.js); on a
  // phone-sized viewport the fitted terminal settles at a small enough
  // rows/cols that internal/tui/model.go's own responsive layout collapses
  // the sidebar and the Command hub's tab strip entirely (confirmed
  // empirically: at Pixel 7's 412x839 viewport the rendered terminal ends
  // up ~372x358px, and its text contains neither "Command" nor
  // "Approvals" nor "reviewer" once layout and xterm's resize settle) --
  // exactly the same real, content-driven responsive behavior a physical
  // terminal app would show in that little space, not a bug to route
  // around. Desktop already exercises the identical WASM binary and key
  // bindings; skip here rather than assert against a viewport the real
  // product's own layout logic doesn't support this interaction at.
  test.skip(isMobile, "the real TUI's responsive layout needs more grid than a phone-sized terminal fits");
  await page.goto("/");
  const controlSection = page.locator("#live-tui");
  await controlSection.scrollIntoViewIfNeeded();
  await page.getByRole("button", { name: /Launch the Control Room/ }).click();

  const terminal = page.locator(".control-terminal");
  await expect(terminal).toBeVisible();

  // xterm.js (no canvas/webgl addon is installed -- see
  // sites/landing/public/tui/wasm-bridge.js and package.json's
  // dependencies) renders its default DOM renderer here: real
  // ".xterm-rows" text nodes Playwright can assert against, not just a
  // canvas. Confirmed empirically by this very assertion passing against
  // getByText, not merely assumed from the addon list.
  await expect(terminal.locator(".xterm-rows")).toBeVisible({ timeout: 20_000 });

  // Real seeded content from cmd/agent-comms-tui-wasm/seed.go, not
  // decorative: reviewer is one of the three demo agents the workforce
  // table renders, and "Approvals" is the Command hub's fourth tab label,
  // visible on the very first (Overview) screen before any navigation.
  // Scoped to the terminal: "reviewer"/"Approvals" both also appear
  // elsewhere on the static landing page (the walkthrough reel, the mode
  // map, ...), so an unscoped getByText is ambiguous -- this is real
  // xterm.js DOM content, not the surrounding marketing page.
  await expect(terminal.getByText("reviewer", { exact: false }).first()).toBeVisible({ timeout: 20_000 });
  await expect(terminal.getByText("Approvals", { exact: false }).first()).toBeVisible();

  // Every rendered row must start at the terminal's left edge. xterm.js
  // trims trailing spaces, so an inherited text-align (the hero centres its
  // text) used to centre each short row on its own and scatter the TUI.
  const rowOffsets = await terminal.locator(".xterm-rows").evaluate((rows) => {
    const left = rows.getBoundingClientRect().left;
    return [...rows.children]
      .filter((row) => row.textContent?.trim())
      .map((row) => Math.round((row.firstElementChild ?? row).getBoundingClientRect().left - left));
  });
  expect(rowOffsets.length).toBeGreaterThan(10);
  expect(Math.max(...rowOffsets)).toBeLessThanOrEqual(1);

  // Drive the real keybinding into the seeded Approvals row list and
  // confirm the pending approval is actually there and actionable.
  await terminal.click();
  for (let i = 0; i < 3; i++) {
    await page.keyboard.press("]");
  }
  await page.keyboard.press("Enter");
  // "PEND" rather than the full "PENDING": the STATUS column truncates to
  // fit its width at this viewport ("🟡 PENDI…"), confirmed empirically by
  // a screenshot of the real render -- asserting a prefix that survives
  // truncation is more robust than assuming the full word always fits.
  await expect(terminal.getByText(/PEND/i).first()).toBeVisible();
  // RFC 0039: the seeded agent is "claude-reviewer", so the approval's
  // action is agent.activate:claude-reviewer. The approval's own ID
  // (approval-orchestrator-reviewer) is a separate literal and unchanged.
  await expect(terminal.getByText("agent.activate:claude-reviewer", { exact: false }).first()).toBeVisible();

  // Reject the seeded approval (key "x") -- a real, signed, terminal state
  // transition (approvals.go's appReject -> approval.reject), not a
  // decorative animation. Confirm the confirmation prompt rendered from
  // real Go source text (rowlist.go's confirmYesLabel) before signing.
  await page.keyboard.press("x");
  await expect(terminal.getByText(/Sign and apply/i).first()).toBeVisible();
  await page.keyboard.press("y");

  // The rendered output must actually change as a result: the same
  // approval row now reads REJECTED instead of PENDING (both truncated to
  // fit the STATUS column, so matched by prefix the same way as above) --
  // proof this is a live, stateful program responding to real input, not
  // a screenshot or a canned animation.
  await expect(terminal.getByText(/PEND/i)).toHaveCount(0, { timeout: 10_000 });
  await expect(terminal.getByText(/REJ/i).first()).toBeVisible();
});

test("reveals the footer after a reload", async ({ page }) => {
  await page.goto("/");
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  await page.reload();

  const footer = page.locator(".site-footer");
  await footer.scrollIntoViewIfNeeded();
  await expect(footer).toHaveClass(/is-revealed/);
  await expect(footer.getByRole("link", { name: "Agent Comms home" })).toBeVisible();
  await expect(footer.getByRole("navigation", { name: "Footer navigation" })).toBeVisible();
});

test("offers the supported installer commands without direct binary actions", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("/download");

  await expect(page.getByRole("heading", { level: 1, name: /Agent Comms, ready to run/ })).toBeVisible();
  await expect(page.getByText("Verified release", { exact: true })).toBeVisible();
  await expect(page.locator("[data-copy-command]")).toHaveCount(3);
  await expect(page.locator("code").filter({ hasText: "install.sh" })).toBeVisible();
  await expect(page.locator("code").filter({ hasText: "install.ps1" })).toBeVisible();
  await expect(page.getByRole("link", { name: /Download Agent Comms/ })).toHaveCount(0);

  await page.getByRole("button", { name: "Copy Linux + macOS install command" }).click();
  await expect(page.getByRole("button", { name: "Copy Linux + macOS install command" })).toContainText("Command copied");
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain("install.sh");

  await expect(page.getByText(/every governed project/i)).toBeVisible();
});

test("offers a build-from-source card with inline commands, not a prebuilt dev channel", async ({ page }) => {
  await page.goto("/download");

  await expect(page.locator("#nightly")).toHaveCount(0);
  await expect(page.getByRole("heading", { level: 2, name: "Building from source?" })).toBeVisible();
  await expect(page.getByText("FOR CONTRIBUTORS", { exact: true })).toBeVisible();
  await expect(page.locator("code").filter({ hasText: "git clone" })).toBeVisible();
  const contributingLink = page.getByRole("link", { name: /Other shipped binaries/i });
  await expect(contributingLink).toHaveAttribute("href", /CONTRIBUTING\.md#build-from-source/);
});

test("does not present build-from-source as a third installer", async ({ page }) => {
  await page.goto("/download");

  // Only the two real installers appear in the numbered INSTALL INDEX --
  // build-from-source is a distinct, separately-labeled path for
  // contributors, not an equal-weight "03."
  const installIndexItems = page.locator("#installer").getByRole("listitem");
  await expect(installIndexItems).toHaveCount(2);
  await expect(page.getByRole("heading", { level: 2, name: "Linux + macOS" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 2, name: "Windows" })).toBeVisible();
});

test("reveals and activates installer rows as they enter the viewport", async ({ page }) => {
  await page.goto("/download");

  const unixInstaller = page.locator('[data-reveal="download-unix"]');
  await unixInstaller.scrollIntoViewIfNeeded();
  await expect(unixInstaller).toHaveClass(/is-revealed/);
  await expect(unixInstaller).toHaveClass(/is-active/);
});

test("hydrates the download page without animation class drift", async ({ page }) => {
  const hydrationErrors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error" && message.text().toLowerCase().includes("hydrat")) {
      hydrationErrors.push(message.text());
    }
  });

  await page.goto("/download");
  await expect(page.locator('[data-reveal="download-intro"]')).toHaveClass(/is-revealed/);
  expect(hydrationErrors).toEqual([]);
});

test("activates the main hero motion after hydration", async ({ page }) => {
  await page.goto("/");

  const hero = page.locator('[data-reveal="hero"]');
  await expect(hero).toHaveClass(/is-revealed/);
  await expect(hero).toHaveClass(/is-active/);
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
});

test("waits for meaningful viewport entry before revealing main sections", async ({ page }) => {
  await page.goto("/");

  const statement = page.locator('[data-reveal="statement"]');
  await expect(statement).not.toHaveClass(/is-revealed/);
  await statement.scrollIntoViewIfNeeded();
  await expect(statement).toHaveClass(/is-revealed/);
  await expect(statement).toHaveClass(/is-active/);
});

test("reveals the releases page", async ({ page }) => {
  await page.goto("/releases");

  const releases = page.locator('[data-reveal="releases-list"]');
  await releases.scrollIntoViewIfNeeded();
  await expect(releases).toHaveClass(/is-revealed/);
  await expect(releases).toHaveClass(/is-active/);
});

test("returns a branded not-found response", async ({ page }) => {
  const response = await page.goto("/missing-page");

  expect(response?.status()).toBe(404);
  await expect(page.getByRole("heading", { level: 1, name: "This page left the project scope." })).toBeVisible();
  await expect(page.getByRole("link", { name: /Return home/ })).toHaveAttribute("href", "/");
});

test("footer links to native pages instead of bouncing straight to GitHub", async ({ page }) => {
  await page.goto("/");

  const footer = page.locator(".site-footer");
  await expect(footer.getByRole("link", { name: "Releases", exact: true })).toHaveAttribute("href", "/releases");
  await expect(footer.getByRole("link", { name: "Security", exact: true })).toHaveAttribute("href", "/security");
  await expect(footer.getByRole("link", { name: "License", exact: true })).toHaveAttribute("href", "/license");
  await expect(footer.getByRole("link", { name: "Contact", exact: true })).toHaveAttribute("href", "/support");
  await expect(footer.getByRole("link", { name: "Privacy", exact: true })).toHaveAttribute("href", "/privacy");
  await expect(footer.getByRole("link", { name: "Report an issue", exact: true })).toHaveAttribute(
    "href",
    "/support#report-issue"
  );
  await expect(footer.getByRole("link", { name: "Changelog", exact: true })).toHaveAttribute(
    "href",
    "https://agentcomms-docs.vercel.app/releases/changelog/"
  );
  await expect(footer.getByRole("link", { name: "GitHub", exact: true })).toHaveAttribute("href", "https://github.com/DhanushSantosh/AgentComms");
});

test("keeps beta history on docs and prepares the stable release view", async ({ page }) => {
  await page.goto("/releases");

  await expect(page.getByRole("heading", { level: 1, name: /Nothing ships without a changelog/ })).toBeVisible();
  await expect(page.getByRole("heading", { name: "The first stable release is being prepared." })).toBeVisible();
  await expect(page.locator("main")).not.toContainText(/v0\.\d+\.\d+/);
  await expect(page.getByRole("link", { name: /Browse the beta release archive/ })).toHaveAttribute("href", "https://agentcomms-docs.vercel.app/releases/changelog/#beta-archive");
  await expect(page.getByRole("link", { name: /Read the full changelog/ })).toBeVisible();
});

test("shows the full Apache 2.0 text on the license page", async ({ page }) => {
  await page.goto("/license");

  await expect(page.getByRole("heading", { level: 1 })).toContainText("Use it. Modify it.");
  await expect(page.getByText("Commercial use", { exact: true })).toBeVisible();
  await expect(page.getByText(/Apache License/).first()).toBeVisible();
  await expect(page.getByText(/TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION/)).toBeVisible();
});

test("points to private advisories on the security page", async ({ page }) => {
  await page.goto("/security");

  await expect(page.getByRole("heading", { level: 1 })).toContainText("Found a flaw?");
  await expect(page.locator("#advisory-url")).toHaveText("https://github.com/DhanushSantosh/AgentComms/security/advisories/new");
  await expect(page.getByRole("button", { name: /Copy the private advisory link/ })).toBeVisible();
});

test("cross-links support and privacy pages to the security policy", async ({ page }) => {
  await page.goto("/support");

  await expect(page.getByRole("heading", { level: 1 })).toContainText("Stuck?");
  await expect(page.getByRole("link", { name: "security policy" })).toHaveAttribute("href", "/security");

  await page.goto("/privacy");
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Nothing to disclose");
  await expect(page.getByRole("link", { name: "security policy" })).toHaveAttribute("href", "/security");
});

test("shows a breadcrumb trail and a working back button on sub-pages", async ({ page }) => {
  await page.goto("/license");

  const breadcrumb = page.getByRole("navigation", { name: "Breadcrumb" });
  await expect(breadcrumb.getByRole("link", { name: "Home" })).toHaveAttribute("href", "/");
  await expect(breadcrumb.getByText("License", { exact: true })).toBeVisible();

  await page.goto("/");
  await page.goto("/security");
  await page.getByRole("button", { name: "Back" }).click();
  await expect(page).toHaveURL("/");
});
