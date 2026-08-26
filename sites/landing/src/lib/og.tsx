import { ImageResponse } from "next/og";
import { site } from "@/lib/site";

// Shared by every route's opengraph-image.tsx (RFC 0021). Next resolves
// each of those to a fixed PNG at export time since none of this site's
// routes are dynamic -- fully compatible with next.config.ts's
// `output: "export"`, no server involved at request time.
export const ogSize = { width: 1200, height: 630 } as const;
export const ogContentType = "image/png" as const;

const ink = "#000000";
const text = "#d7e5e3";
const cyan = "#56d6c9";
const steel = "#78918f";

export function renderOgImage(title: string, kicker: string): ImageResponse {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          background: ink,
          padding: "72px",
          fontFamily: "sans-serif"
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: "18px" }}>
          <svg width="58" height="42" viewBox="0 0 42 30" fill="none">
            <path
              d="M3 5h15l5 5h16M3 15h36M3 25h15l5-5h16"
              stroke={text}
              strokeWidth="2.4"
              strokeLinecap="square"
            />
            <circle cx="3" cy="5" r="2.6" fill={cyan} />
            <circle cx="3" cy="15" r="2.6" fill={cyan} />
            <circle cx="3" cy="25" r="2.6" fill={cyan} />
          </svg>
          <span style={{ color: text, fontSize: "30px", fontWeight: 700, letterSpacing: "-0.01em" }}>
            Agent Comms
          </span>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: "22px" }}>
          <span style={{ color: cyan, fontSize: "24px", letterSpacing: "0.08em", textTransform: "uppercase" }}>
            {kicker}
          </span>
          <span style={{ color: text, fontSize: "64px", fontWeight: 700, lineHeight: 1.1, letterSpacing: "-0.02em" }}>
            {title}
          </span>
        </div>
        <span style={{ color: steel, fontSize: "22px" }}>{new URL(site.marketingSiteUrl).host}</span>
      </div>
    ),
    ogSize
  );
}
