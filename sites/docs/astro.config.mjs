import { defineConfig } from "astro/config";
import mdx from "@astrojs/mdx";
import sitemap from "@astrojs/sitemap";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const site = process.env.DOCS_SITE_URL ?? "https://docs.agentcomms.dev";
const marketingSite = process.env.PUBLIC_MARKETING_SITE_URL ?? "https://agentcomms.dev";
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
// Before v1, every release is beta-maturity regardless of branch -- main's
// "stable"/dev's "next" distinction only becomes truthful once a 1.x release
// actually ships. Explicit PUBLIC_DOCS_CHANNEL still wins for any deployment
// that wants to force a specific label.
const isPreV1 = productVersion.split(".")[0] === "0";
const docsChannel = process.env.PUBLIC_DOCS_CHANNEL ?? (isPreV1 ? "beta" : (sourceRef === "main" ? "stable" : "next"));

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
      "import.meta.env.PUBLIC_SOURCE_BRANCH": JSON.stringify(sourceRef === "main" ? "main" : "dev"),
      "import.meta.env.PUBLIC_MARKETING_SITE_URL": JSON.stringify(marketingSite),
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
