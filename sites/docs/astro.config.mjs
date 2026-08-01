import { defineConfig } from "astro/config";
import mdx from "@astrojs/mdx";
import sitemap from "@astrojs/sitemap";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const site = process.env.DOCS_SITE_URL ?? "https://docs.agentcomms.dev";
const repositoryRoot = fileURLToPath(new URL("../..", import.meta.url));

function gitOutput(arguments_) {
  try {
    return execFileSync("git", arguments_, { cwd: repositoryRoot, encoding: "utf8" }).trim();
  } catch {
    return "";
  }
}

const releaseTag = gitOutput(["describe", "--tags", "--abbrev=0", "--match", "v[0-9]*"]);
const productVersion = process.env.PUBLIC_PRODUCT_VERSION ?? releaseTag.replace(/^v/, "");
const sourceRef = process.env.GITHUB_REF_NAME ?? gitOutput(["branch", "--show-current"]);
const docsChannel = process.env.PUBLIC_DOCS_CHANNEL ?? (sourceRef === "main" ? "stable" : "next");

if (!productVersion) {
  throw new Error("Documentation builds require PUBLIC_PRODUCT_VERSION or an accessible version tag.");
}

export default defineConfig({
  site,
  output: "static",
  integrations: [mdx(), sitemap()],
  vite: {
    define: {
      "import.meta.env.PUBLIC_DOCS_CHANNEL": JSON.stringify(docsChannel),
      "import.meta.env.PUBLIC_PRODUCT_VERSION": JSON.stringify(productVersion)
    }
  },
  markdown: {
    shikiConfig: {
      themes: {
        light: "github-light",
        dark: "github-dark-default"
      },
      wrap: true
    }
  }
});
