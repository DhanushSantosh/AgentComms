# Agent Comms documentation site

This is a custom Astro application. Astro supplies content collections and static rendering; the layouts, navigation, search interface, components, and CSS are owned by Agent Comms.

Public product content lives in `docs/site`. RFCs, backlog items, and maintainer procedures are deliberately outside the site's content collection.

```sh
npm install
npm run docs:generate
npm run docs:dev
```

Before submitting a change:

```sh
npm run docs:generate:check
npm run docs:check
npm run docs:test
npm run docs:lighthouse
```

`main` is the stable documentation channel and `dev` is next. A deployment must set `PUBLIC_DOCS_CHANNEL`, `PUBLIC_PRODUCT_VERSION`, and `DOCS_SITE_URL` explicitly.
