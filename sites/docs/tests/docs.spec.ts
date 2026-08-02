import { expect, test } from "@playwright/test";

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

test("mobile navigation exposes the current manual tree", async ({ page }, testInfo) => {
  test.skip(!testInfo.project.name.startsWith("mobile"), "mobile-only interaction");
  await page.goto("/agents/invocations/");
  await page.getByRole("button", { name: "Open documentation navigation" }).click();
  await expect(page.getByRole("complementary", { name: "Documentation navigation" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Invocation lifecycle", exact: true })).toHaveAttribute("aria-current", "page");
});

test("the TUI recording stays inside the article and viewport", async ({ page }) => {
  await page.goto("/start/tui/");
  const recording = page.getByRole("img", { name: /terminal control room/ });
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
