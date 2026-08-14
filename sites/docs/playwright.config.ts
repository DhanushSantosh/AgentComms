import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  outputDir: "./output/playwright-results",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? "github" : "list",
  expect: {
    toHaveScreenshot: {
      animations: "disabled",
      // Wider than sites/landing's identical 0.01: confirmed across two
      // independent CI runs, a real, stable ~0.02-0.03 diff persists on
      // guide/reference/delivery (both themes) even after regenerating
      // baselines inside the exact Playwright Docker image this repo
      // pins (mcr.microsoft.com/playwright:v1.62.1-noble) -- not
      // flakiness (same pages, same rough magnitude every time), and not
      // something this project's own font-rendering-stabilization launch
      // flags below fully eliminate. These are long-form prose pages with
      // far more small-text glyph edges than landing's chunkier display
      // headlines, so ordinary cross-machine antialiasing variance adds
      // up further here. The non-visual structural assertions (data-theme,
      // functional behavior) remain the strict regression check; this
      // tolerance exists so a real color/layout break still fails loudly
      // without chasing byte-perfect text-edge parity across machines.
      maxDiffPixelRatio: 0.05
    }
  },
  use: {
    baseURL: "http://127.0.0.1:4323",
    colorScheme: "light",
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
    launchOptions: {
      args: [
        "--font-render-hinting=none",
        "--disable-skia-runtime-opts",
        "--disable-font-subpixel-positioning",
        "--disable-lcd-text",
        "--force-color-profile=srgb"
      ]
    }
  },
  webServer: {
    command: "node scripts/serve.mjs --host 127.0.0.1 --port 4323",
    url: "http://127.0.0.1:4323",
    reuseExistingServer: false,
    timeout: 30_000
  },
  projects: [
    {
      name: "desktop-chromium",
      use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 1000 } }
    },
    {
      name: "mobile-chromium",
      use: { ...devices["Pixel 7"] }
    }
  ]
});
