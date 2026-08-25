import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import satori from "satori";
import { Resvg } from "@resvg/resvg-js";

// RFC 0021: per-page OG images for the docs site. Astro has no built-in
// equivalent of Next's opengraph-image.tsx file convention, so this uses
// satori + @resvg/resvg-js directly -- the same rendering engine `next/og`
// (used by sites/landing's own opengraph-image.tsx routes) uses internally,
// so the two sites' cards match visually.
//
// satori's bundled font parser can't read the variable woff2 files
// sites/docs/public/fonts/ serves to real visitors (verified directly: it
// throws on any variable-font `fvar` table, woff2 or ttf, regardless of
// format). sites/docs/og-fonts/ vendors genuine static-instance TTF cuts of
// the same two OFL-licensed families (see THIRD_PARTY_FONTS.md) used only
// by this build-time script -- never served to a browser, so this doesn't
// reintroduce the font-CDN request self-hosting was meant to avoid.
// import.meta.url doesn't survive Astro's build-time bundling here (the
// compiled chunk ends up nested under dist/.prerender/chunks/, so a path
// relative to it resolves to the wrong place) -- process.cwd() is reliable
// instead, since astro build always runs from this package's own directory
// (npm --workspace @agent-comms/docs), mirroring sites/landing/src/lib/
// repo.ts's identical fix for the identical bundling problem.
const ogFontsDir = resolve(process.cwd(), "og-fonts");

const ink = "#000000";
const text = "#d7e5e3";
const cyan = "#56d6c9";
const steel = "#78918f";

// Same fallback astro.config.mjs itself uses for `site`.
const docsHost = new URL(process.env.DOCS_SITE_URL ?? "https://agentcomms-docs.vercel.app").host;

let cachedFonts: Awaited<ReturnType<typeof loadFonts>> | undefined;

async function loadFonts() {
  const [manropeRegular, manropeBold, displayBold] = await Promise.all([
    readFile(resolve(ogFontsDir, "manrope-400.ttf")),
    readFile(resolve(ogFontsDir, "manrope-700.ttf")),
    readFile(resolve(ogFontsDir, "bricolage-grotesque-700.ttf"))
  ]);
  return [
    { name: "Manrope", data: manropeRegular, weight: 400 as const, style: "normal" as const },
    { name: "Manrope", data: manropeBold, weight: 700 as const, style: "normal" as const },
    { name: "Bricolage Grotesque", data: displayBold, weight: 700 as const, style: "normal" as const }
  ];
}

export async function renderOgImage(title: string, kicker: string): Promise<Buffer> {
  cachedFonts ??= await loadFonts();

  // satori's TS signature types its element parameter as React's
  // `ReactNode`, but its runtime accepts the same plain hyperscript-style
  // object tree used below independent of React -- this project has no
  // React dependency in sites/docs (unlike sites/landing, which really is
  // Next/React and uses real JSX for the equivalent tree in src/lib/og.tsx).
  // The cast reflects that type/runtime mismatch, not an actual `any`.
  const svg = await satori(
    {
      type: "div",
      props: {
        style: {
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          background: ink,
          padding: "72px",
          fontFamily: "Manrope"
        },
        children: [
          {
            type: "div",
            props: {
              style: { display: "flex", alignItems: "center", gap: "18px" },
              children: [
                {
                  type: "svg",
                  props: {
                    width: 58,
                    height: 42,
                    viewBox: "0 0 42 30",
                    fill: "none",
                    children: [
                      {
                        type: "path",
                        props: {
                          d: "M3 5h15l5 5h16M3 15h36M3 25h15l5-5h16",
                          stroke: text,
                          strokeWidth: "2.4",
                          strokeLinecap: "square"
                        }
                      },
                      { type: "circle", props: { cx: "3", cy: "5", r: "2.6", fill: cyan } },
                      { type: "circle", props: { cx: "3", cy: "15", r: "2.6", fill: cyan } },
                      { type: "circle", props: { cx: "3", cy: "25", r: "2.6", fill: cyan } }
                    ]
                  }
                },
                {
                  type: "span",
                  props: {
                    style: { color: text, fontSize: "30px", fontWeight: 700, letterSpacing: "-0.01em" },
                    children: "Agent Comms"
                  }
                }
              ]
            }
          },
          {
            type: "div",
            props: {
              style: { display: "flex", flexDirection: "column", gap: "22px" },
              children: [
                {
                  type: "span",
                  props: {
                    style: {
                      color: cyan,
                      fontSize: "22px",
                      letterSpacing: "0.08em",
                      textTransform: "uppercase"
                    },
                    children: kicker
                  }
                },
                {
                  type: "span",
                  props: {
                    style: {
                      color: text,
                      fontSize: "58px",
                      fontFamily: "Bricolage Grotesque",
                      fontWeight: 700,
                      lineHeight: 1.15,
                      letterSpacing: "-0.02em"
                    },
                    children: title
                  }
                }
              ]
            }
          },
          {
            type: "span",
            props: { style: { color: steel, fontSize: "22px" }, children: docsHost }
          }
        ]
      }
    } as unknown as Parameters<typeof satori>[0],
    { width: 1200, height: 630, fonts: cachedFonts }
  );

  const resvg = new Resvg(svg, { fitTo: { mode: "width", value: 1200 } });
  return resvg.render().asPng();
}
