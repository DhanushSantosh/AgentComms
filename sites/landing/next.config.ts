import { PHASE_DEVELOPMENT_SERVER } from "next/constants";
import type { NextConfig } from "next";

export default function nextConfig(phase: string): NextConfig {
  const isDev = phase === PHASE_DEVELOPMENT_SERVER;
  return {
    distDir: isDev ? ".next" : "dist",
    env: {
      NEXT_PUBLIC_DOCS_URL: process.env.NEXT_PUBLIC_DOCS_URL ?? "https://agentcomms-docs.vercel.app",
      NEXT_PUBLIC_SITE_URL: process.env.NEXT_PUBLIC_SITE_URL ?? "https://agentcomms-cli.vercel.app"
    },
    images: { unoptimized: true },
    output: "export",
    trailingSlash: true,
    // Next rejects dev-server requests (including the HMR websocket) whose
    // Origin header isn't in this list -- without it, even 127.0.0.1 gets
    // blocked once bound to 0.0.0.0 for LAN/Tailscale access, which looks
    // like the page silently hanging (HMR retries forever, nothing errors
    // visibly). SITES_PUBLIC_HOST comes from the untracked .env.local so a
    // developer's own LAN/Tailscale IP never needs to be hardcoded here.
    ...(isDev && {
      allowedDevOrigins: ["127.0.0.1", "localhost", ...(process.env.SITES_PUBLIC_HOST ? [process.env.SITES_PUBLIC_HOST] : [])]
    })
  };
}
