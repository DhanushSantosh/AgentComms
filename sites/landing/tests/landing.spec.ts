import { expect, test } from "@playwright/test";

test("presents the product thesis and truthful lifecycle", async ({ page }) => {
  await page.goto("/");

  await expect(page.getByRole("heading", { level: 1, name: /Let agents work at once/ })).toBeVisible();
  await expect(page.getByText("Keep the project in one piece.")).toBeVisible();
  await expect(page.getByRole("link", { name: /Install Agent Comms/ })).toHaveAttribute("href", "/download");
  await expect(page.getByRole("link", { name: "Docs", exact: true }).first()).toHaveAttribute("href", "https://agentcomms-docs.vercel.app");

  const lifecycle = page.locator(".lifecycle-orbit");
  await expect(lifecycle).toContainText("DELIVERED");
  await expect(lifecycle).toContainText("ACKNOWLEDGED");
  await expect(lifecycle).toContainText("COMPLETED");
  await expect(page.getByText(/A transport can succeed while the agent never acknowledges/)).toBeVisible();
});

test("mobile navigation opens, closes, and preserves keyboard semantics", async ({ page, isMobile }) => {
  test.skip(!isMobile, "Mobile-only navigation behavior");
  await page.goto("/");

  const toggle = page.getByRole("button", { name: "Open navigation" });
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByRole("navigation", { name: "Primary navigation" })).toBeVisible();
  await page.getByRole("link", { name: "Protocol" }).click();
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
});

test("supports a keyboard skip path", async ({ page }) => {
  await page.goto("/");
  await page.keyboard.press("Tab");
  const skipLink = page.getByRole("link", { name: "Skip to content" });
  await expect(skipLink).toBeFocused();
  await skipLink.press("Enter");
  await expect(page.locator("#main-content")).toBeFocused();
});

test("resolves a simulated scope collision", async ({ page }) => {
  await page.goto("/#collision");

  await expect(page.getByText("WITHOUT COORDINATION", { exact: true })).toBeVisible();
  await expect(page.getByText("CONFLICT DETECTED", { exact: false })).toBeVisible();
  await expect(page.getByText("WITH AGENT COMMS", { exact: true })).toBeVisible();
  await expect(page.getByText("SCOPE LEASE GRANTED", { exact: false })).toBeVisible();
});

test("walkthrough scenes can be selected and replayed", async ({ page }) => {
  await page.goto("/#demo");

  const reel = page.locator("[data-demo-reel]");
  await page.getByRole("button", { name: /03.*AGENT ACK/ }).click();
  await expect(reel).toHaveAttribute("data-scene", "2");
  await expect(reel.locator("[data-reel-live]")).toContainText(/explicitly accepts the obligation/);
  await page.getByRole("button", { name: "Replay handoff evidence film" }).click();
  await expect(reel).toHaveAttribute("data-scene", "0");
});

test("selected feature visuals preserve exact product semantics", async ({ page }) => {
  await page.goto("/");

  const stream = page.locator(".authority-stream");
  await expect(stream.getByText("agent-comms — live authority", { exact: true })).toBeVisible();
  await expect(stream.getByText("chain verified", { exact: true })).toBeVisible();

  const orbit = page.locator(".lifecycle-orbit");
  await expect(orbit.getByText("Delivered ≠ Acknowledged", { exact: true }).first()).toBeVisible();
});

test("relay separates transport from acknowledgement and returns a result", async ({ page }) => {
  await page.goto("/#relay");

  const relay = page.locator("[data-relay-sequence]");
  await page.getByRole("button", { name: "Replay agent relay demonstration" }).click();
  await expect(relay).toHaveAttribute("data-relay-state", "requested");
  await expect(relay).toHaveAttribute("data-relay-state", "delivered", { timeout: 3_000 });
  await expect(relay.getByText("DELIVERED ≠ ACKNOWLEDGED")).toBeVisible();
  await expect(relay).toHaveAttribute("data-relay-state", "completed", { timeout: 7_000 });
  await expect(relay.getByText("24 / 24 auth tests pass", { exact: true })).toBeVisible();
});

test("control room resolves a human-tier approval coherently", async ({ page }) => {
  await page.goto("/#control");

  const frame = page.locator("[data-tui-frame]");
  await page.getByRole("button", { name: /approval-orchestrator-axiom/ }).click();
  await expect(page.getByText("HUMAN AUTHORITY REQUIRED")).toBeVisible();
  await page.getByRole("button", { name: "Approve with human authority" }).click();
  await expect(frame).toHaveAttribute("data-control-state", "approved");
  await expect(frame.locator("[data-control-role]")).toHaveText("ORCHESTRATOR");
  await expect(frame.locator("[data-control-event] b")).toHaveText("approval.approve");
});

test("keeps delivery evidence separate from acknowledgement", async ({ page }) => {
  await page.goto("/#protocol");

  const orbit = page.locator(".lifecycle-orbit");
  await expect(orbit.getByText("DELIVERED", { exact: true })).toBeVisible();
  await expect(orbit.getByText("ACKNOWLEDGED", { exact: true })).toBeVisible();
  await expect(orbit.getByText("Delivered ≠ Acknowledged")).toBeVisible();
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
  await expect(page.getByText("Verification assets incomplete", { exact: true })).toBeVisible();
  await expect(page.locator("[data-copy-command]")).toHaveCount(3);
  await expect(page.locator("code").filter({ hasText: "install.sh" })).toBeVisible();
  await expect(page.locator("code").filter({ hasText: "install.ps1" })).toBeVisible();
  await expect(page.getByRole("link", { name: /Download Agent Comms/ })).toHaveCount(0);

  await page.getByRole("button", { name: "Copy Linux + macOS install command" }).click();
  await expect(page.getByRole("button", { name: "Copy Linux + macOS install command" })).toContainText("Command copied");
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain("install.sh");

  await expect(page.getByText(/every governed project/i)).toBeVisible();
});

test("surfaces the nightly build command, distinct from the release installers", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("/download");

  const nightly = page.locator("#nightly");
  await expect(nightly).toContainText("FOR DEVELOPERS");
  await expect(nightly.locator("code").filter({ hasText: "oras pull" })).toBeVisible();

  await nightly.getByRole("button", { name: "Copy nightly build command" }).click();
  await expect(nightly.getByRole("button", { name: "Copy nightly build command" })).toContainText("Command copied");
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain("ghcr.io/dhanushsantosh/agentcomms-nightly");
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
  const stream = page.locator(".authority-stream");
  await expect(hero).toHaveClass(/is-revealed/);
  await expect(hero).toHaveClass(/is-active/);
  await stream.scrollIntoViewIfNeeded();
  await expect(stream).toBeVisible();
});

test("waits for meaningful viewport entry before revealing main sections", async ({ page }) => {
  await page.goto("/");

  const statement = page.locator('[data-reveal="statement"]');
  await expect(statement).not.toHaveClass(/is-revealed/);
  await statement.scrollIntoViewIfNeeded();
  await expect(statement).toHaveClass(/is-revealed/);
  await expect(statement).toHaveClass(/is-active/);
});

test("reveals the releases section on the download page", async ({ page }) => {
  await page.goto("/download");

  const releases = page.locator('[data-reveal="releases"]');
  await releases.scrollIntoViewIfNeeded();
  await expect(releases).toHaveClass(/is-revealed/);
  await expect(releases).toHaveClass(/is-active/);
  await expect.poll(() => releases.locator(".release-list").evaluate((element) => getComputedStyle(element, "::after").animationName)).toBe("release-ledger-scan");
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
    "https://github.com/DhanushSantosh/AgentComms/issues/new"
  );
  await expect(footer.getByRole("link", { name: "Changelog", exact: true })).toHaveAttribute(
    "href",
    "https://agentcomms-docs.vercel.app/releases/changelog/"
  );
  await expect(footer.getByRole("link", { name: "GitHub", exact: true })).toHaveAttribute("href", "https://github.com/DhanushSantosh/AgentComms");
});

test("lists every tagged release on the releases page", async ({ page }) => {
  await page.goto("/releases");

  await expect(page.getByRole("heading", { level: 1, name: /Nothing ships without a changelog/ })).toBeVisible();
  await expect(page.getByText("v0.3.0", { exact: true })).toBeVisible();
  await expect(page.getByText("v0.2.1", { exact: true })).toBeVisible();
  await expect(page.getByText("v0.2.0", { exact: true })).toBeVisible();
  await expect(page.getByText("v0.1.0", { exact: true })).toBeVisible();
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
  await expect(page.getByRole("link", { name: /Open a private advisory/ })).toHaveAttribute(
    "href",
    "https://github.com/DhanushSantosh/AgentComms/security/advisories/new"
  );
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
