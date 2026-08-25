# RFC 0021: Installable PWA shell and per-page SEO for the landing and docs sites

## Status

**Implemented on `dev`, 2026-08-25.** Per `docs/rfcs/README.md` and `docs/development-workflow.md`'s
design-proposal rule, reviewed and accepted before implementation began. This RFC sits outside the
README's listed trigger categories (no public contract, schema, governance, identity, signing,
transaction, installation-security, supported-platform, or TUI-navigation change) -- written as an
RFC anyway at the project owner's explicit request, for the same reviewed-before-built discipline
the rest of the project's site work has followed informally in commit history (e.g. the
TUI-design-language unification, RFC-less, across `ff0cef2` and `786523a`). Scope confirmed
directly with the project owner beforehand: both sites, an installable shell without offline
caching, dynamic per-page OG images, and Organization + SoftwareApplication structured data (see
"Alternatives considered" for the options not taken). The design below was built largely as
proposed, with real corrections found during implementation, recorded here rather than silently
edited into the original proposal:

1. **Icon source changed mid-implementation, directed by the project owner.** The proposal as
   written implied rasterizing each site's existing `public/favicon.svg`. Doing that first and
   showing the result surfaced a real mismatch: landing's `favicon.svg` colors its three dots
   cyan/lilac/coral and docs' `favicon.svg` is a different glyph shape entirely (a "P"-style
   mark) -- neither matches the brand mark actually rendered in either site's own header
   (`BrandMark.tsx`/`BrandMark.astro`: one shared path, all-cyan dots, light stroke). Rejected in
   favor of building the icon set directly from that live header mark's real geometry and colors,
   so the installed-app icon matches what a visitor already sees on the page.
2. **`opengraph-image.tsx` routes need `export const dynamic = "force-static"` under
   `output: "export"`**, the same requirement `sitemap.ts`/`robots.ts` already had -- not called
   out explicitly in the original proposal; the static export build fails closed without it
   (caught immediately by `npm run build`, not a silent gap).
3. **satori (the docs site's OG-image renderer) cannot parse any variable font at all**, woff2 or
   ttf -- verified directly, not assumed: it throws on the `fvar` table regardless of container
   format. The variable woff2 files `sites/docs/public/fonts/` already serves to real visitors
   were never going to work for this. Fix: `sites/docs/og-fonts/` vendors genuine static-instance
   TTF cuts of the same two OFL-licensed families (Manrope, Bricolage Grotesque), fetched once
   from Google Fonts' own static-instance endpoint, used only by the build-time OG script and
   documented in `THIRD_PARTY_FONTS.md` -- never served to a browser, so this doesn't reintroduce
   the font-CDN dependency self-hosting the variable fonts was meant to avoid.
4. **Lighthouse no longer has a `pwa` category at all** in `lighthouse@13` (this repo's pinned
   version) -- confirmed by reading its default config directly, not assumed from an older
   version's docs. The `installable-manifest`/`maskable-icon`/`themed-omnibox`/`splash-screen`
   audits the original "Test and rollout plan" meant to gate on on are gone from Lighthouse core
   entirely, not just recategorized. Replaced with direct, real assertions instead (new
   `tests/pwa-seo.spec.ts` on both sites): the manifest resolves and parses with the expected
   icon set, every icon link the page declares actually resolves, each sampled page's real
   `og:image` route resolves with the correct `image/png` content type, and the JSON-LD parses
   with the expected `@type`s. This is a strictly more precise test of the same thing the
   Lighthouse category would have checked, not a downgrade.
5. **Writing that last test surfaced a real, pre-existing bug**, unrelated to anything new in this
   RFC except that OG images are the first extensionless static route either site's local preview
   server (`scripts/serve.mjs`) had to serve: Next's file-convention output for `opengraph-image`
   ships with no file extension at all (`dist/opengraph-image`, `dist/download/opengraph-image`),
   so `serve.mjs`'s extension-keyed content-type lookup fell through to
   `application/octet-stream`. Fixed with magic-byte sniffing for the extensionless case, landing
   site only (docs' own `og/*.png` routes carry a real `.png` extension and were never affected).
6. **`social-card.svg` was deleted outright on both sites, not kept** as the original proposal's
   Compatibility section speculated ("remains only as the manifest's non-maskable reference
   icon"). Once every `og:image` reference was migrated to the new per-page routes, nothing in
   either codebase referenced the file any longer -- confirmed by grep before deleting, not
   assumed.
7. **`sharp` (icon rasterization) and `png-to-ico` (real multi-resolution `.ico` packaging, not
   named in the original proposal -- sharp itself cannot emit `.ico`) were added as explicit root
   `devDependencies`.** `sharp` was already resolvable transitively (hence the original proposal's
   "no new dependency" framing), but relying on an undeclared transitive dependency for a real
   script is fragile -- if the upstream package that pulled it in ever drops it, the icon script
   breaks with no warning. Declaring it directly costs nothing (identical version already
   vendored) and removes that fragility.

**One claim in "Test and rollout plan" could not be fulfilled as written and is corrected here
rather than quietly**: the platform-validator check (pasting a live deployed URL into a real
unfurl-preview tool) needs an actual deployed URL, and this work lands on `dev` before the next
deploy -- not possible to run yet. What was actually done instead: every OG image route was
verified end to end through the real static build output (`npm run build` then reading the actual
generated PNG bytes, not a mock), both sites' full `check` pipelines pass, and the new
`pwa-seo.spec.ts` suite exercises the manifest/icons/OG-image/JSON-LD against a real served build
on both sites (12/12 passing), plus the full pre-existing Playwright suites on both sites (29/29
passing, 3 pre-existing skips unrelated to this change) and the landing visual-baseline suite (no
snapshot changes needed, as predicted). The platform-validator check against a real deployed URL
still needs to happen once this reaches an actual deployment, and is not blocking merge to `dev`.

## Context

`sites/landing` (Next.js, static export) and `sites/docs` (Astro, static output) both already
have real per-page `<title>`/description metadata, canonical URLs, sitemaps
(`src/app/sitemap.ts`, `@astrojs/sitemap`), and `robots.txt`. What's missing was found by reading
both sites' actual `<head>` output and `public/` contents rather than assumed:

1. **No PWA surface at all.** Neither site has a web app manifest, and the only icon asset either
   ships is a single `favicon.svg` -- no `apple-touch-icon`, no 192/512 PNG icons, no maskable
   icon, no `favicon.ico` fallback. Neither is installable (Add to Home Screen / desktop install
   prompt) and neither passes a Lighthouse PWA audit.
2. **`og:image` points at an SVG, on both sites, and it's the same file on every page.**
   `sites/landing/public/social-card.svg` and `sites/docs/public/social-card.svg` are each
   referenced from every route's Open Graph/Twitter metadata (`layout.tsx`'s `openGraph.images`,
   `BaseLayout.astro`'s `image` prop default). This is a real interoperability bug, not just a
   missing feature: Twitter/X, Facebook, LinkedIn, Slack, and Discord's unfurl crawlers do not
   render SVG for `og:image` -- they require PNG/JPG, and silently show no image or a broken one
   instead. Every shared link from either site today gets this treatment, and every page shares
   one generic card regardless of its actual title.
3. **No structured data.** Neither site emits any JSON-LD -- no `Organization`, no
   `SoftwareApplication` -- so neither is eligible for the rich-result/knowledge-panel treatment
   Google can offer a real open-source project's homepage.

**Desired outcome:** both sites become installable (pass Lighthouse's PWA checks), every page
gets a real branded OG image carrying its own title instead of one shared generic SVG that
doesn't even render on most platforms, and both sites carry the structured data Google can
actually use -- all as a build-time-only addition with no new runtime server and no service
worker, since these are static marketing/documentation sites, not applications with a genuine
offline-use case.

## Proposed design

Three independent additions, implemented natively per framework rather than through one shared
abstraction across Next.js and Astro -- the two frameworks have genuinely different idiomatic
mechanisms for generated images and static metadata, and forcing a shared layer over both would
fight each one for no real reuse benefit (the two sites don't share a build pipeline today, and
this doesn't create a reason for them to start).

### 1. Icon set and web manifest

A one-off script, `scripts/generate-icons.mjs`, run manually whenever the brand mark itself
changes (not on every build), uses `sharp` (already present as a root-level `package.json`
override, so no new dependency) to rasterize the existing brand-mark SVG into the fixed set
each platform actually consumes:

- `favicon.ico` (multi-resolution: 16/32/48)
- `apple-touch-icon.png` (180x180)
- `icon-192.png`, `icon-512.png`
- `icon-512-maskable.png` (rendered with the safe-zone padding the maskable-icon spec requires,
  not a naive re-export of the full-bleed mark)

Outputs are committed to each site's `public/` directly -- generated artifacts checked into
source, the same way `sites/landing/public/tui/` already commits its own build output rather
than regenerating it from scratch on every CI run.

Each site gets its own `site.webmanifest` (distinct `name`/`short_name` per site, the icon array
above, `theme_color`/`background_color` matching each site's existing dark palette --
`#071216` landing, `#0c1419` docs, both already set as `theme-color` today --
`display: "standalone"`, `start_url: "/"`). Linked via `<link rel="manifest">` and
`<link rel="apple-touch-icon">` in `sites/landing/src/app/layout.tsx` and
`sites/docs/src/layouts/BaseLayout.astro`, alongside the existing `favicon.svg` link (kept, not
replaced -- SVG favicons are the right format for browser tabs, the bug is specifically
`og:image`, not favicon usage).

### 2. Dynamic per-page OG images

- **Landing (Next.js)**: the native `opengraph-image.tsx` file convention (`next/og`'s
  `ImageResponse`, built into Next -- no added package). One file per route
  (`app/opengraph-image.tsx`, `app/download/opengraph-image.tsx`, `app/security/
  opengraph-image.tsx`, etc.), rendering that route's real title over the same dark panel
  treatment the rest of the site uses. Because every route in this site is static (no dynamic
  segments), Next resolves each to a fixed PNG at export time -- compatible with the existing
  `output: "export"` config with no server involved at request time.
- **Docs (Astro)**: Astro has no equivalent built-in convention, so a single endpoint,
  `src/pages/og/[...slug].png.ts`, uses `satori` + `@resvg/resvg-js` (the same rendering engine
  `next/og` uses internally, so the two sites' cards match visually) with `getStaticPaths`
  sourcing every doc's real title from the existing `content.config.ts` collection, and
  `export const prerender = true` so every one of them is baked to a static PNG at build time,
  same as the landing side. Uses the woff2 fonts already vendored in `sites/docs/public/fonts/`
  -- no new font dependency.

Both sites' `openGraph.images`/`og:image` metadata switches from `social-card.svg` to the
per-page generated PNG; `social-card.svg` remains only as the manifest's non-maskable reference
icon and README/repo-level use, not as an `og:image` value anywhere.

### 3. Structured data

A JSON-LD `<script type="application/ld+json">`, injected in each layout:

- **`Organization`** (name, logo, `sameAs`: the GitHub repository URL) -- site-wide on both
  sites.
- **`SoftwareApplication`** (name, description, `operatingSystem`, `applicationCategory:
  DeveloperApplication`, `license`, download URL) -- landing homepage and `/download` only.

No `AggregateRating`, `Offer`, or pricing schema -- this project has none of those properties,
and inventing schema data that doesn't correspond to anything real is itself a Google Search
Console violation risk (structured-data spam), not just unnecessary.

## Alternatives considered

- **Static PNG cards instead of per-page generation.** Export the existing `social-card.svg`
  (and maybe two or three section variants) to PNG once by hand. Simpler -- no `next/og`/
  `satori` build step to maintain -- but every page keeps sharing a generic card instead of
  showing its own title, which is the actual problem being fixed here, not just the file format.
  Rejected in favor of per-page generation; confirmed directly with the project owner.
- **Full offline-capable PWA (service worker + precaching).** Would make both sites genuinely
  usable offline and speed up repeat visits. Rejected: these are static marketing/documentation
  sites, not applications anyone needs offline, and a service worker adds a real new class of
  bug (stale cached content surviving a deploy, cache-versioning logic, update-prompt UX) to
  every future release for a use case that doesn't exist today. The manifest/icon work alone
  already earns Lighthouse's PWA-installability checks and the Add to Home Screen / desktop
  install affordance, which is the actual thing being asked for.
- **One shared icon/OG-generation package across both sites.** Rejected: Next.js's
  `opengraph-image.tsx` convention and Astro's endpoint-based static generation are different
  enough mechanisms that a shared abstraction would mean maintaining a compatibility shim
  between two frameworks that otherwise don't share build tooling today, for two call sites
  total. Shared visual constants (colors, wordmark layout) can still be duplicated in both
  small enough that keeping them as plain literals in each file is more legible than an
  abstraction neither site otherwise needs.
- **Landing site only, docs left as-is.** Rejected: the SVG `og:image` bug and missing manifest
  are identical problems on both sites, and docs pages get shared just as often as landing
  pages. Confirmed with the project owner to cover both.

## Compatibility and rollout

- No change to either site's routing, existing metadata (titles/descriptions/canonicals/
  sitemaps), or build output format -- both sites remain fully static exports, deployed exactly
  as today (`npm run landing:build` / `npm run docs:build`, `sites/*/vercel.json`).
- `favicon.svg` stays as the favicon; nothing currently depending on it changes.
- `sites/landing/package.json` gains no new dependency (`next/og` ships with Next itself).
  `sites/docs/package.json` gains two new dependencies: `satori` and `@resvg/resvg-js`.
- Root `package.json`'s existing `sharp` override is reused by `scripts/generate-icons.mjs`;
  that script is not part of the `sites:build`/`landing:build`/`docs:build` pipeline -- it's a
  manual, occasionally-run tool, and its outputs are committed like `public/tui/` already is.
- Both sites' existing Playwright visual-snapshot suites (`tests/visual.spec.ts`) are unaffected
  -- new `<head>` tags and new `/og/*`/`opengraph-image` routes don't touch rendered page pixels,
  so no snapshot updates are needed for those. New, narrow Playwright checks are added instead
  (see "Test and rollout plan").

## Security and privacy implications

- No new runtime surface: every new artifact (icons, manifest, OG images, JSON-LD) is generated
  at build time and served as a static file, identical in kind to every other file already in
  `public/`/`dist/`. No new request-time code path exists on either site.
- The web manifest and OG images contain only public marketing copy already present on each
  page (title, description) -- no user data, no secrets, nothing not already publicly visible in
  the page itself.
- `satori`/`@resvg/resvg-js` run only at build time, in CI/local dev, never in a served request
  path -- the same trust boundary the project's other build-time-only tooling
  (`agent-comms-docgen`, `pagefind`) already sits inside.

## Test and rollout plan

- `scripts/lighthouse.mjs` (both sites already run this) extended to assert the PWA-
  installability and SEO Lighthouse categories, not just performance -- a real regression gate,
  not just a one-time manual check.
- New Playwright assertions on both sites: the manifest link resolves and parses as valid JSON
  with the expected icon set; a sample of per-page OG image URLs resolve with a 200 and a
  `image/png` content type; the JSON-LD script tag is present and parses as valid JSON on the
  homepage.
- Manual verification before landing on `dev`: paste a real deployed URL from each site into at
  least one platform validator (e.g. Twitter Card Validator or the equivalent open unfurl-preview
  tooling) to confirm the PNG OG image actually renders where the SVG did not -- the specific bug
  this RFC exists to fix, verified against a real consumer, not just a 200 status code.
- `npm run landing:check` / `npm run docs:check` (existing full build+typecheck+test gates) must
  pass with the new routes/files included before this is considered done.

## Unresolved questions

- Whether the docs site's per-doc OG images should visually distinguish sections (Guide vs.
  Reference vs. Operations) beyond just the page title -- left as a possible future refinement,
  not required for this RFC's actual goal (every page getting a real, renderable, on-brand card
  instead of one shared non-rendering SVG).
- Whether either site should eventually add `Article`/`TechArticle` JSON-LD per docs page
  (beyond the site-wide `Organization` this RFC adds) -- deferred; not needed to close the actual
  gap found (zero structured data today), and best evaluated once real Search Console data shows
  whether Google is already surfacing docs pages well without it.
