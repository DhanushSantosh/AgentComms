import { expect, test } from "@playwright/test";

const visualSnapshotTimeoutMilliseconds = 15_000;

test("landing page visual baselines", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/");
  await page.evaluate(() => document.fonts.ready);
  await expect(page).toHaveScreenshot("landing-hero.png", {
    timeout: visualSnapshotTimeoutMilliseconds
  });
  await page.goto("/#protocol");
  await expect(page).toHaveScreenshot("landing-protocol.png", {
    timeout: visualSnapshotTimeoutMilliseconds
  });
  await page.goto("/#control");
  await expect(page).toHaveScreenshot("landing-control-room.png", {
    timeout: visualSnapshotTimeoutMilliseconds
  });
  await page.goto("/download");
  await page.evaluate(() => document.fonts.ready);
  await expect(page).toHaveScreenshot("download-page.png", {
    timeout: visualSnapshotTimeoutMilliseconds
  });
  const handoff = page.locator('[data-reveal="download-handoff"]');
  await handoff.scrollIntoViewIfNeeded();
  await expect(handoff).toHaveScreenshot("download-handoff.png", {
    timeout: visualSnapshotTimeoutMilliseconds
  });
});
