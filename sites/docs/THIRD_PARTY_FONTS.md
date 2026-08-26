# Documentation font licenses

The documentation website self-hosts the same type system as the Agent Comms product site:

- Bricolage Grotesque by Mathieu Triay.
- Manrope by Mikhail Sharanda.
- Commit Mono by Eirik Brenna.

All three fonts are distributed under the SIL Open Font License 1.1. They are served from local assets, so documentation visits do not disclose requests to a font CDN.

`og-fonts/` additionally vendors static-instance TTF cuts of Manrope and Bricolage Grotesque (same OFL 1.1 license, sourced from Google Fonts' static CDN endpoint), used only at build time by `src/lib/og.ts` to render per-page OG images (RFC 0021). satori's bundled font parser cannot read the variable woff2 files above -- it throws on any variable font's `fvar` table. These static instances are never served to a browser, so they don't reintroduce the font-CDN dependency self-hosting the variable fonts above was meant to avoid.
