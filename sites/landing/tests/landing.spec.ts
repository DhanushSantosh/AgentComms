import { expect, test } from "@playwright/test";

test("presents the product thesis and truthful lifecycle", async ({ page }) => {
  await page.goto("/");

  await expect(page.getByRole("heading", { level: 1, name: /Let agents work at once/ })).toBeVisible();
  await expect(page.getByText("Keep the project in one piece.")).toBeVisible();
  await expect(page.getByRole("link", { name: /Install Agent Comms/ })).toHaveAttribute("href", "#install");
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

test("returns a branded not-found response", async ({ page }) => {
  const response = await page.goto("/missing-page");

  expect(response?.status()).toBe(404);
  await expect(page.getByRole("heading", { level: 1, name: "This page left the project scope." })).toBeVisible();
  await expect(page.getByRole("link", { name: /Return home/ })).toHaveAttribute("href", "/");
});
