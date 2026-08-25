const fallbackMarketingSiteUrl = "https://agentcomms-cli.vercel.app";
const fallbackDocumentationUrl = "https://agentcomms-docs.vercel.app";
const productVersion = process.env.NEXT_PUBLIC_PRODUCT_VERSION;

if (!productVersion) {
  throw new Error("Landing runtime metadata requires NEXT_PUBLIC_PRODUCT_VERSION.");
}

export const site = {
  documentationUrl: process.env.NEXT_PUBLIC_DOCS_URL ?? fallbackDocumentationUrl,
  marketingSiteUrl: process.env.NEXT_PUBLIC_SITE_URL ?? fallbackMarketingSiteUrl,
  productVersion,
  // Shared with sites/landing/src/lib/downloads.ts's own copy -- kept here
  // too since RFC 0021's structured data (Organization/SoftwareApplication
  // JSON-LD) needs it independent of the download-page-specific data that
  // module builds around it.
  repositoryUrl: "https://github.com/DhanushSantosh/AgentComms"
} as const;

export function documentationPage(path: string): string {
  return new URL(path, `${site.documentationUrl}/`).toString();
}

// Shared header nav for standalone content pages (releases, license, security,
// support, privacy) -- these have no in-page sections worth anchoring to, so
// they cross-link each other instead of the homepage's scroll-spy anchors.
export const utilityNavItems = [
  { label: "Download", href: "/download" },
  { label: "Releases", href: "/releases" },
  { label: "Security", href: "/security" },
  { label: "License", href: "/license" },
  { label: "Support", href: "/support" }
] as const;
