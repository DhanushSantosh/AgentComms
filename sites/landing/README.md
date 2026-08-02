# Agent Comms landing site

The marketing site is a standalone Next.js App Router application. It uses static export, server-rendered content, and a small deferred browser module for the live product instruments. Its visual system and responsive behavior remain specific to Agent Comms, and it can be built or deployed independently from the documentation application.

```sh
npm run landing:dev
npm run landing:build
npm run landing:check
npm run landing:test
npm run landing:lighthouse
npm run sites:dev
```

`sites:dev` starts the landing site at `http://127.0.0.1:3000` and the documentation site at `http://127.0.0.1:4321`, with reciprocal local links.
Set `SITES_DEV_HOST`, `LANDING_DEV_PORT`, or `DOCS_DEV_PORT` when those defaults are already occupied.

The production canonical defaults to `https://agentcomms-cli.vercel.app`, while documentation defaults to `https://agentcomms-docs.vercel.app`. Override them with `NEXT_PUBLIC_SITE_URL` and `NEXT_PUBLIC_DOCS_URL`. Product version metadata comes from `PUBLIC_PRODUCT_VERSION` or the most recent accessible repository tag.

The landing page has no React client components. Its small progressive interactions live in `public/landing.js`. After static export, a fail-closed post-build step removes the unused Next.js router and Flight runtime from exported HTML, while retaining Next.js routing, metadata, local-font, image, and build behavior. Adding a React client component requires removing that optimization and accepting the corresponding runtime budget.
