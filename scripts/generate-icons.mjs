#!/usr/bin/env node
// Rasterizes the app icon set (favicon.ico, apple-touch-icon.png, icon-192.png,
// icon-512.png, icon-512-maskable.png) documented in RFC 0021, for both sites.
//
// Deliberately does NOT read either site's public/favicon.svg as the source:
// landing's favicon.svg colors its three dots cyan/lilac/coral and docs'
// favicon.svg is a different glyph shape entirely -- neither matches the
// brand mark actually rendered in either site's own header
// (sites/landing/src/components/BrandMark.tsx,
// sites/docs/src/components/BrandMark.astro), which is the same path shape
// on both sites, stroked in the shared --text/--chrome-ink tone
// (#d7e5e3) with all three dots in the shared --cyan/--signal tone
// (#56d6c9), per both sites' globals.css. The icon set is built from that
// live mark directly so the installed-app icon matches what visitors
// already see in the header, not a separate, older design.
//
// This is a manual, occasionally-run tool, not part of the sites:build
// pipeline -- outputs are committed to each site's public/ directly, the
// same way sites/landing/public/tui/ already commits its own generated
// build output rather than regenerating it on every CI run. Re-run only if
// the shared brand mark itself changes.
//
// Usage: node scripts/generate-icons.mjs <landing|docs>

import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import sharp from "sharp";
import pngToIco from "png-to-ico";

const site = process.argv[2];
if (site !== "landing" && site !== "docs") {
  console.error("Usage: node scripts/generate-icons.mjs <landing|docs>");
  process.exit(1);
}

const repoRoot = resolve(import.meta.dirname, "..");
const publicDir = resolve(repoRoot, "sites", site, "public");

// Same path/circle geometry as BrandMark.tsx/BrandMark.astro (viewBox
// "0 0 42 30"), centered inside a square canvas with a black rounded-rect
// backing -- the same treatment both sites' existing favicon.svg already
// uses for its own canvas (rx proportional to the 64x64 original's 13px).
function markSvg(size) {
  const scale = (size / 64) * 1.05;
  const tx = (size - 42 * scale) / 2;
  const ty = (size - 30 * scale) / 2;
  const strokeWidth = 3.4 / scale;
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">
  <rect width="${size}" height="${size}" rx="${size * 0.2}" fill="#000000"/>
  <g transform="translate(${tx} ${ty}) scale(${scale})">
    <path d="M3 5h15l5 5h16M3 15h36M3 25h15l5-5h16" fill="none" stroke="#d7e5e3" stroke-width="${strokeWidth}" stroke-linecap="square"/>
    <circle cx="3" cy="5" r="2.4" fill="#56d6c9"/>
    <circle cx="3" cy="15" r="2.4" fill="#56d6c9"/>
    <circle cx="3" cy="25" r="2.4" fill="#56d6c9"/>
  </g>
</svg>`;
}

async function renderPng(outputName, size, { maskablePadding = 0 } = {}) {
  const outputPath = resolve(publicDir, outputName);
  if (maskablePadding > 0) {
    // Maskable icons need the mark to sit inside a safe zone (the spec's
    // "safe zone" is the inner ~80% of the canvas) so platforms that crop
    // to a circle/rounded-square don't clip the mark itself.
    const inner = Math.round(size * (1 - maskablePadding * 2));
    const innerPng = await sharp(Buffer.from(markSvg(inner)), { density: 384 })
      .resize(inner, inner)
      .png()
      .toBuffer();
    await sharp({ create: { width: size, height: size, channels: 4, background: { r: 0, g: 0, b: 0, alpha: 1 } } })
      .composite([{ input: innerPng, gravity: "center" }])
      .png()
      .toFile(outputPath);
    return;
  }
  await sharp(Buffer.from(markSvg(size)), { density: 384 }).resize(size, size).png().toFile(outputPath);
}

await mkdir(publicDir, { recursive: true });

await renderPng("apple-touch-icon.png", 180);
await renderPng("icon-192.png", 192);
await renderPng("icon-512.png", 512);
await renderPng("icon-512-maskable.png", 512, { maskablePadding: 0.1 });

// favicon.ico bundles the classic multi-resolution set browsers still fall
// back to when they don't use the SVG favicon (RSS readers, some crawlers,
// older browser chrome).
const icoSizes = [16, 32, 48];
const icoPngs = await Promise.all(
  icoSizes.map((size) => sharp(Buffer.from(markSvg(size)), { density: 384 }).resize(size, size).png().toBuffer())
);
const ico = await pngToIco(icoPngs);
await writeFile(resolve(publicDir, "favicon.ico"), ico);

console.log(`Generated icon set for ${site} in ${publicDir}`);
