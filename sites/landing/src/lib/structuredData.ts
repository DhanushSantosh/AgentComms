import { site } from "@/lib/site";

// RFC 0021: SoftwareApplication JSON-LD -- only true on the homepage and
// /download (a specific installable product), not repeated on every
// content page the way the site-wide Organization block in layout.tsx is.
export const softwareApplicationJsonLd = {
  "@context": "https://schema.org",
  "@type": "SoftwareApplication",
  name: "Agent Comms",
  description:
    "Agent Comms is the project authority for concurrent coding agents. Control ownership, deliver work directly, and verify every handoff.",
  applicationCategory: "DeveloperApplication",
  operatingSystem: "Linux, macOS, Windows",
  license: "https://github.com/DhanushSantosh/AgentComms/blob/main/LICENSE",
  downloadUrl: new URL("/download", site.marketingSiteUrl).toString(),
  url: site.marketingSiteUrl
} as const;
