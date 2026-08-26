import { expect, test } from "@playwright/test";

// RFC 0021: real installability/OG assertions in place of Lighthouse's PWA
// category, which lighthouse@13 (this repo's pinned version) no longer
// scores at all -- confirmed directly by reading its default config, not
// assumed: `installable-manifest`/`maskable-icon`/`themed-omnibox`/
// `splash-screen` are gone from Lighthouse core, not just recategorized.

test("ships a valid, installable web manifest", async ({ page, request }) => {
  await page.goto("/");
  const manifestHref = await page.locator('link[rel="manifest"]').getAttribute("href");
  expect(manifestHref).toBe("/site.webmanifest");

  const response = await request.get(manifestHref!);
  expect(response.ok()).toBe(true);
  const manifest = await response.json();
  expect(manifest.name).toBeTruthy();
  expect(manifest.display).toBe("standalone");
  const sizes = manifest.icons.map((icon: { sizes: string }) => icon.sizes);
  expect(sizes).toContain("192x192");
  expect(sizes).toContain("512x512");
  expect(manifest.icons.some((icon: { purpose?: string }) => icon.purpose === "maskable")).toBe(true);
});

test("every icon link the homepage declares actually resolves", async ({ page, request }) => {
  await page.goto("/");
  for (const selector of ['link[rel="icon"]', 'link[rel="apple-touch-icon"]']) {
    for (const href of await page.locator(selector).evaluateAll((els) => els.map((el) => el.getAttribute("href")))) {
      const response = await request.get(href!);
      expect(response.ok(), `${href} should resolve`).toBe(true);
    }
  }
});

for (const [path, imageRoute] of [
  ["/", "/opengraph-image"],
  ["/download", "/download/opengraph-image"],
  ["/security", "/security/opengraph-image"]
] as const) {
  test(`${path} has a real per-page PNG og:image`, async ({ page, request }) => {
    await page.goto(path);
    const ogImage = await page.locator('meta[property="og:image"]').getAttribute("content");
    expect(ogImage).toContain(imageRoute);

    const response = await request.get(imageRoute);
    expect(response.ok()).toBe(true);
    expect(response.headers()["content-type"]).toBe("image/png");
  });
}

test("homepage declares Organization and SoftwareApplication JSON-LD", async ({ page }) => {
  await page.goto("/");
  const scripts = await page.locator('script[type="application/ld+json"]').allTextContents();
  const types = scripts.map((json) => JSON.parse(json)["@type"]);
  expect(types).toContain("Organization");
  expect(types).toContain("SoftwareApplication");
});
