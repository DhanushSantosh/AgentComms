import { expect, test } from "@playwright/test";

test("presents the product thesis and truthful lifecycle", async ({ page }) => {
  await page.goto("/");

  await expect(page.getByRole("heading", { level: 1, name: /Let agents work at once/ })).toBeVisible();
  await expect(page.getByText("Keep the project in one piece.")).toBeVisible();
  await expect(page.getByRole("link", { name: /Install Agent Comms/ })).toHaveAttribute("href", "/download");
  await expect(page.getByRole("link", { name: "Docs", exact: true }).first()).toHaveAttribute("href", "https://docs.agentcomms.dev");

  const lifecycle = page.getByRole("list").filter({ hasText: "REQUESTED" });
  await expect(lifecycle).toContainText("DELIVERED");
  await expect(lifecycle).toContainText("CLAIMED");
  await expect(lifecycle).toContainText("COMPLETED");
  await expect(page.getByText(/A transport can succeed while the agent never acknowledges/)).toBeVisible();
});

test("copies the install command", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("/#install");

  await page.getByRole("button", { name: "Copy command" }).click();
  await expect(page.getByRole("button", { name: "Command copied" })).toBeVisible();
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain("install.sh");
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

  await expect(page.getByText("COLLISION", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "With Agent Comms" }).click();
  await expect(page.getByText("RESOLVED", { exact: true })).toBeVisible();
  await expect(page.getByText("one owner", { exact: true })).toBeVisible();
});

test("keeps delivery evidence separate from acknowledgement", async ({ page }) => {
  await page.goto("/#protocol");

  await page.getByRole("button", { name: "DELIVERED transport evidence" }).click();
  await expect(page.getByText("The selected transport acted and returned bounded delivery evidence.")).toBeVisible();
  await expect(page.getByText("semantic consumption", { exact: true })).toBeVisible();
  await expect(page.getByText("invocation.notify", { exact: true })).toBeVisible();
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
  const coordinationField = page.locator("[data-motion-stage]");
  await expect(hero).toHaveClass(/is-revealed/);
  await expect(hero).toHaveClass(/is-active/);
  await coordinationField.scrollIntoViewIfNeeded();
  await expect(coordinationField).toHaveClass(/is-revealed/);
  await expect(coordinationField).toHaveClass(/is-active/);
  await expect(coordinationField.locator(".path:not(.path--authority)").first()).toHaveCSS("animation-name", "field-flow");
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
