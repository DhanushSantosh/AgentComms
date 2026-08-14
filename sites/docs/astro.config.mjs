import { defineConfig } from "astro/config";
import mdx from "@astrojs/mdx";
import sitemap from "@astrojs/sitemap";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const site = process.env.DOCS_SITE_URL ?? "https://agentcomms-docs.vercel.app";
const marketingSite = process.env.PUBLIC_MARKETING_SITE_URL ?? "https://agentcomms-cli.vercel.app";
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
  server: { host: true },
  vite: {
    define: {
      "import.meta.env.PUBLIC_DOCS_CHANNEL": JSON.stringify(docsChannel),
      "import.meta.env.PUBLIC_SOURCE_BRANCH": JSON.stringify(sourceRef === "main" ? "main" : "dev"),
      "import.meta.env.PUBLIC_MARKETING_SITE_URL": JSON.stringify(marketingSite),
      "import.meta.env.PUBLIC_PRODUCT_VERSION": JSON.stringify(productVersion)
    },
    // Vite's dev server rejects requests whose Host header isn't in this
    // list as DNS-rebinding protection -- mirrors sites/landing's
    // next.config.ts allowedDevOrigins fix for the same class of problem
    // (a LAN/Tailscale IP counts as a foreign host once the server is
    // bound to every interface). SITES_PUBLIC_HOST comes from the
    // untracked .env.local so a developer's own IP never needs to be
    // hardcoded here.
    server: {
      allowedHosts: process.env.SITES_PUBLIC_HOST ? [process.env.SITES_PUBLIC_HOST] : undefined
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
