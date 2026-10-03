import { expect, test } from "@playwright/test";
import { build } from "esbuild";
import { fileURLToPath } from "node:url";

test("stable release picker renders only the selected entry", async ({ page }) => {
  const fixture = await build({
    stdin: {
      contents: `import React from "react";
        import { createRoot } from "react-dom/client";
        import { ReleaseHistory } from "./src/components/ReleaseHistory";
        createRoot(document.getElementById("fixture")).render(React.createElement(ReleaseHistory, { releases: [
          { version: "v1.1.0", channel: "STABLE", name: "Second", date: "2026-10-04", dateLabel: "4 Oct 2026", highlights: ["Second release notes"] },
          { version: "v1.0.0", channel: "STABLE", name: "First", date: "2026-10-03", dateLabel: "3 Oct 2026", highlights: ["First release notes"] }
        ] }));`,
      resolveDir: fileURLToPath(new URL("..", import.meta.url)),
      loader: "tsx"
    },
    bundle: true, write: false, format: "iife", jsx: "automatic",
    plugins: [{ name: "fixture-styles", setup(builder) {
      builder.onResolve({ filter: /\.module\.css$/ }, () => ({ path: "styles", namespace: "fixture" }));
      builder.onLoad({ filter: /.*/, namespace: "fixture" }, () => ({ contents: "export default {};", loader: "js" }));
    } }]
  });
  const script = fixture.outputFiles?.[0]?.text;
  if (!script) throw new Error("Stable release fixture did not compile");
  await page.setContent('<main id="fixture"></main>');
  await page.addScriptTag({ content: script });
  await expect(page.getByRole("heading", { name: "v1.1.0", exact: true })).toBeVisible();
  await expect(page.getByRole("article")).toHaveCount(1);
  await page.getByLabel("Release", { exact: true }).selectOption("v1.0.0");
  await expect(page.getByRole("heading", { name: "v1.0.0", exact: true })).toBeVisible();
  await expect(page.getByText("Second release notes", { exact: true })).toHaveCount(0);
  await expect(page.getByText("First release notes", { exact: true })).toBeVisible();
  await expect(page.getByRole("article")).toHaveCount(1);
});
