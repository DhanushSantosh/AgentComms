import type { Metadata, Viewport } from "next";
import localFont from "next/font/local";
import type { ReactNode } from "react";
import { MotionHydrationBridge } from "@/components/MotionHydrationBridge";
import { site } from "@/lib/site";
import "./globals.css";

const displayFont = localFont({
  src: "./fonts/bricolage-grotesque-latin-variable.woff2",
  variable: "--font-display",
  display: "swap",
  weight: "200 800"
});

const bodyFont = localFont({
  src: "./fonts/manrope-latin-variable.woff2",
  variable: "--font-body",
  display: "swap",
  weight: "200 800"
});

const monoFont = localFont({
  src: [
    { path: "./fonts/commit-mono-latin-400.woff2", weight: "400" },
    { path: "./fonts/commit-mono-latin-700.woff2", weight: "700" }
  ],
  variable: "--font-mono",
  display: "swap"
});

const fontVariables = [displayFont.variable, bodyFont.variable, monoFont.variable].join(" ");

const title = "Agent Comms — Keep concurrent agents in one coherent project";
const description = "Agent Comms is the project authority for concurrent coding agents. Control ownership, deliver work directly, and verify every handoff.";

export const metadata: Metadata = {
  metadataBase: new URL(site.marketingSiteUrl),
  title,
  description,
  alternates: { canonical: "/" },
  manifest: "/site.webmanifest",
  icons: {
    icon: [
      { url: "/favicon.svg", type: "image/svg+xml" },
      { url: "/favicon.ico", sizes: "48x48" }
    ],
    apple: "/apple-touch-icon.png"
  },
  openGraph: {
    type: "website",
    title,
    description: "Project authority, direct agent relay, and signed evidence for concurrent coding agents.",
    url: "/"
  },
  twitter: { card: "summary_large_image", title, description }
};

// RFC 0021: Organization JSON-LD, site-wide -- SoftwareApplication is added
// separately on the homepage and /download only, where it's actually true
// (a specific installable product), not duplicated on every content page.
const organizationJsonLd = {
  "@context": "https://schema.org",
  "@type": "Organization",
  name: "Agent Comms",
  url: site.marketingSiteUrl,
  logo: new URL("/icon-512.png", site.marketingSiteUrl).toString(),
  sameAs: [site.repositoryUrl]
};

export const viewport: Viewport = { themeColor: "#071216", width: "device-width", initialScale: 1 };

export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: JSON.stringify(organizationJsonLd) }}
        />
        <script dangerouslySetInnerHTML={{ __html: `document.documentElement.classList.add("motion-ready");` }} />
        {/*
          LiveControlRoom lazy-loads Task 4's public/tui/wasm-bridge.js at
          runtime via a native, unbundled `import()`. That file in turn
          imports "@xterm/xterm" and "@xterm/addon-fit" as bare specifiers,
          which only resolve in a browser via an import map -- the npm
          packages themselves ship CommonJS/UMD, not ES modules, so this
          points those specifiers at the real ESM bundles
          scripts/build-tui-wasm.mjs produces in public/tui/vendor/. Must
          stay in <head>, ahead of any module script/import() on the page.
        */}
        <script
          type="importmap"
          dangerouslySetInnerHTML={{
            __html: JSON.stringify({
              imports: {
                "@xterm/xterm": "/tui/vendor/xterm.js",
                "@xterm/addon-fit": "/tui/vendor/addon-fit.js"
              }
            })
          }}
        />
      </head>
      <body className={fontVariables}>
        <MotionHydrationBridge />
        {children}
        <script src="/landing.js" defer />
      </body>
    </html>
  );
}
