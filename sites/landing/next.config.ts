import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import { PHASE_DEVELOPMENT_SERVER } from "next/constants";
import type { NextConfig } from "next";

function readLatestReleaseTag(): string {
  try {
    return execFileSync("git", ["describe", "--tags", "--abbrev=0", "--match", "v[0-9]*"], {
      cwd: resolve(import.meta.dirname, "../.."),
      encoding: "utf8"
    }).trim();
  } catch {
    return "";
  }
}

const releaseTag = readLatestReleaseTag();
const productVersion = process.env.PUBLIC_PRODUCT_VERSION ?? releaseTag.replace(/^v/, "");

if (!productVersion) {
  throw new Error("Landing builds require PUBLIC_PRODUCT_VERSION or an accessible version tag.");
}

export default function nextConfig(phase: string): NextConfig {
  return {
    distDir: phase === PHASE_DEVELOPMENT_SERVER ? ".next" : "dist",
    env: {
      NEXT_PUBLIC_DOCS_URL: process.env.NEXT_PUBLIC_DOCS_URL ?? "https://docs.agentcomms.dev",
      NEXT_PUBLIC_PRODUCT_VERSION: productVersion,
      NEXT_PUBLIC_SITE_URL: process.env.NEXT_PUBLIC_SITE_URL ?? "https://agentcomms.dev"
    },
    images: { unoptimized: true },
    output: "export",
    trailingSlash: true
  };
}
