import { expect, test } from "@playwright/test";

test("the current release stays visible while archive releases open one at a time", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/releases/changelog/");
  const current = page.locator("#current-release");
  const archive = page.locator("section.release-list", { has: page.locator("#beta-archive") });
  const rows = archive.locator("details.release-row");
  await expect(current.getByRole("heading", { level: 2 })).toContainText("v1.1.0");
  await expect(current.getByRole("heading", { level: 2 })).toContainText("Open Frequencies");
  await expect(current.locator(".release-badge")).toHaveText("Stable");
  await expect(page.locator('[data-release-panel="v1.0.0"]')).toBeVisible();
  // The "On this page" panel is hidden on mobile, so check the links exist.
  await expect(page.locator('a[href="#current-release"]').first()).toHaveText("Current release");
  await expect(page.locator('a[href="#beta-archive"]').first()).toHaveText("Beta archive");
  await expect(rows).toHaveCount(12);
  await expect(archive.locator("details.release-row[open]")).toHaveCount(0);

  const newest = archive.locator('[data-release-panel="v0.8.2"]');
  await newest.locator("summary").click();
  await expect(newest).toHaveAttribute("open", "");
  await expect(current).toBeVisible();
  const oldest = archive.locator('[data-release-panel="v0.1.0"]');
  await oldest.locator("summary").click();
  await expect(oldest).toHaveAttribute("open", "");
  await expect(newest).not.toHaveAttribute("open");
  await expect(current).toBeVisible();

  const anchor = await oldest.locator("h3").getAttribute("id");
  expect(anchor).toBe("v010--the-control-room--beta--2026-07-19");
  await expect(page).toHaveURL(new RegExp(`#${anchor}$`));
  await page.reload();
  await expect(oldest).toHaveAttribute("open", "");
  await expect(oldest.locator(".release-notes")).toBeVisible();
  await expect(current).toBeVisible();

  await page.goto("/releases/changelog/#release-v0-8-1");
  await expect(archive.locator('[data-release-panel="v0.8.1"]')).toHaveAttribute("open", "");

  await expect(page.getByRole("link", { name: /Read the complete archive as Markdown/ })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await oldest.locator("summary").focus();
  await page.keyboard.press("Enter");
  expect(errors).toEqual([]);
});

test("the home page gives humans and agents separate starting paths", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Know who owns the work. Prove what happened next." })).toBeVisible();
  await expect(page.getByRole("link", { name: /Control a project/ })).toHaveAttribute("href", "/start/quickstart/");
  await expect(page.getByRole("link", { name: /Connect an agent/ })).toHaveAttribute("href", "/agents/integrations/");
  await expect(page.getByRole("link", { name: "Agent Comms product website" })).toHaveAttribute("href", "https://agentcomms-cli.vercel.app");
  await expect(page.getByLabel("Invocation lifecycle")).toContainText("Transport evidence");
  await expect(page.getByLabel("Invocation lifecycle")).toContainText("Target acknowledged");
});

test("search opens from the keyboard and filters documentation", async ({ page }) => {
  await page.goto("/");
  await page.keyboard.press("Control+k");
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("searchbox").fill("interactive");
  await expect(dialog.getByRole("link", { name: /Serve an interactive session/ })).toBeVisible();
});

test("theme choice persists and code can be copied", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("/agents/interactive/");
  await page.getByRole("button", { name: "Toggle color theme" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect.poll(() => page.evaluate(() => localStorage.getItem("agent-comms-docs-theme"))).toBe("dark");
  await page.getByRole("button", { name: "Copy code block" }).first().click();
  await expect(page.getByRole("button", { name: "Copy code block" }).first()).toHaveText("Copied");
});

// UX-16 regression test: a denied/unavailable clipboard used to leave the
// copy button's await rejecting with no catch at all -- no feedback that
// anything failed, and no way to tell what to do about it. This is the
// audit's own "clipboard-denied fixture" acceptance criterion.
test("copy failure falls back to selecting the code and says so", async ({ page }) => {
  await page.addInitScript(() => {
    // Simulate a denied permission / unavailable API without needing a
    // real browser permission-denial flow, which Playwright can't easily
    // trigger deterministically across browsers.
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: () => Promise.reject(new Error("denied")) },
      configurable: true,
    });
    // execCommand("copy") also fails in the fallback path, so the failure
    // feedback (not the fallback's own success path) is what's exercised.
    document.execCommand = () => false;
  });
  await page.goto("/agents/interactive/");
  const button = page.getByRole("button", { name: "Copy code block" }).first();
  await button.click();
  await expect(button).toHaveText("Copy failed -- code selected, use Ctrl/Cmd+C");
  // The code itself must actually be selected, so the person can still
  // copy it manually.
  const codeBlock = page.locator(".prose pre").first().locator("code");
  await expect.poll(() => page.evaluate(() => window.getSelection()?.toString().length ?? 0)).toBeGreaterThan(0);
  const selectedText = await page.evaluate(() => window.getSelection()?.toString() ?? "");
  const codeText = await codeBlock.textContent();
  expect(selectedText).toBe(codeText);
  // And it must revert back to "Copy" after the feedback window, not get
  // stuck on the failure message forever.
  await expect(button).toHaveText("Copy", { timeout: 3000 });
});

test("mobile navigation exposes the current manual tree", async ({ page }, testInfo) => {
  test.skip(!testInfo.project.name.startsWith("mobile"), "mobile-only interaction");
  await page.goto("/agents/invocations/");
  await page.getByRole("button", { name: "Open documentation navigation" }).click();
  await expect(page.getByRole("complementary", { name: "Documentation navigation" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Invocation lifecycle", exact: true })).toHaveAttribute("aria-current", "page");
});

test("the TUI recording stays inside the article and viewport", async ({ page }) => {
  await page.goto("/start/tui/");
  const recording = page.getByRole("img", { name: /Current overview separating/ });
  const article = page.locator("article");
  const [recordingBox, articleBox] = await Promise.all([recording.boundingBox(), article.boundingBox()]);

  if (!recordingBox || !articleBox) {
    throw new Error("TUI recording and article must both have visible layout boxes");
  }
  const viewport = page.viewportSize();
  if (!viewport) {
    throw new Error("Playwright project must define a viewport");
  }
  expect(recordingBox.x).toBeGreaterThanOrEqual(articleBox.x);
  expect(recordingBox.x + recordingBox.width).toBeLessThanOrEqual(articleBox.x + articleBox.width + 1);
  expect(recordingBox.x + recordingBox.width).toBeLessThanOrEqual(viewport.width + 1);
  await expect(recording).toHaveAttribute("src", "/tui-overview.png");
  await expect(page.getByRole("link", { name: "Watch the terminal tour" })).toHaveAttribute("href", "/tui-demo.mp4");
});

test("platform tabs and related-page context are keyboard accessible", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("/");
  const windowsTab = page.getByRole("tab", { name: "Windows" });
  await windowsTab.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("tabpanel", { name: "Windows" })).toContainText("install.ps1");
  await page.getByRole("button", { name: "Copy", exact: true }).click();
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain("agent-comms init");

  await page.goto("/start/quickstart/");
  const related = page.getByRole("region", { name: "Continue with" });
  await expect(related.getByRole("link", { name: /TUI control room/ })).toBeVisible();
});

test("syntax tokens use the high-contrast code palette in both themes", async ({ page }) => {
  await page.goto("/start/quickstart/");
  const commandBlock = page.locator(".prose pre").filter({ hasText: "agent-comms status" });
  const firstCommandToken = commandBlock.locator('.line span[style*="--shiki-dark"]').first();
  await expect(firstCommandToken).toHaveCSS("color", "rgb(255, 166, 87)");
  await page.getByRole("button", { name: "Toggle color theme" }).click();
  await expect(firstCommandToken).toHaveCSS("color", "rgb(255, 166, 87)");
});
