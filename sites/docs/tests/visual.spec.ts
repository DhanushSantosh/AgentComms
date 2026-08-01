import { expect, test } from "@playwright/test";

const pages = [
  { name: "home", path: "/" },
  { name: "guide", path: "/guide/agents/" },
  { name: "reference", path: "/reference/cli/" },
  { name: "delivery", path: "/agents/delivery/" }
];

for (const documentedPage of pages) {
  for (const theme of ["light", "dark"] as const) {
    test(`${documentedPage.name} ${theme} visual`, async ({ page }) => {
      await page.addInitScript((selectedTheme) => localStorage.setItem("agent-comms-docs-theme", selectedTheme), theme);
      await page.goto(documentedPage.path);
      await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
      await expect(page).toHaveScreenshot(`${documentedPage.name}-${theme}.png`);
    });
  }
}
