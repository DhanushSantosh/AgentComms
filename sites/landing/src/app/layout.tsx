import type { Metadata, Viewport } from "next";
import localFont from "next/font/local";
import type { ReactNode } from "react";
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
  icons: { icon: "/favicon.svg" },
  openGraph: {
    type: "website",
    title,
    description: "Project authority, direct agent relay, and signed evidence for concurrent coding agents.",
    url: "/",
    images: [{ url: "/social-card.svg", width: 1200, height: 630, alt: "Agent Comms" }]
  },
  twitter: { card: "summary_large_image", title, description, images: ["/social-card.svg"] }
};

export const viewport: Viewport = { themeColor: "#3341f0", width: "device-width", initialScale: 1 };

export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className={fontVariables}>
        {children}
        <script src="/landing.js" defer />
      </body>
    </html>
  );
}
