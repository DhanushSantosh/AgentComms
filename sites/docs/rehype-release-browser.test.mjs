import assert from "node:assert/strict";
import test from "node:test";
import rehypeReleaseBrowser from "./rehype-release-browser.mjs";

const heading = (title) => ({ type: "element", tagName: "h2", properties: {}, children: [{ type: "text", value: title }] });
const paragraph = (value) => ({ type: "element", tagName: "p", properties: {}, children: [{ type: "text", value }] });
const label = (value) => ({ type: "element", tagName: "p", properties: {}, children: [{ type: "element", tagName: "strong", properties: {}, children: [{ type: "text", value }] }] });
const find = (node, predicate) => predicate(node) ? node : (node.children ?? []).map((child) => find(child, predicate)).find(Boolean);
const text = (node) => node.value ?? (node.children ?? []).map(text).join("");

test("the current stable release stays open beside separate earlier and beta lists", () => {
  const tree = { children: [paragraph("Introduction"),
    heading("v1.1.0 — “Second” — Stable — 2026-10-05"), paragraph("New stable"), label("Breaking"), paragraph("Changed"),
    heading("v1.0.0 — “First” — Stable — 2026-10-04"), paragraph("First stable"),
    heading("v1.0.0-rc.1 — “Candidate” — Beta — 2026-10-01"), paragraph("Candidate notes"),
    heading("v0.8.2 — “Order of Arrival” — Beta — 2026-10-02"), paragraph("Beta notes"),
    heading("Verifying a release"), paragraph("Instructions")] };
  rehypeReleaseBrowser()(tree, { path: "/docs/site/releases/changelog.md" });
  const [intro, current, earlier, beta, verifying, instructions] = tree.children;
  assert.equal(text(intro), "Introduction");

  assert.equal(current.properties.id, "current-release");
  const card = current.children[0];
  assert.equal(card.tagName, "article");
  assert.equal(card.properties["data-release-panel"], "v1.1.0");
  const title = find(card, (node) => node.tagName === "h2");
  assert.equal(title.properties.id, "v110--second--stable--2026-10-05", "keeps the plain Markdown heading anchor");
  assert.equal(text(title), "v1.1.0Second");
  assert.equal(text(find(card, (node) => node.tagName === "time")), "5 Oct 2026");
  const breaking = find(card, (node) => node.properties?.className?.includes("release-label"));
  assert.deepEqual([text(breaking), breaking.properties["data-kind"]], ["Breaking", "breaking"]);

  assert.equal(find(earlier, (node) => node.tagName === "h2").properties.id, "earlier-stable-releases");
  assert.deepEqual(earlier.children[2].children.map((row) => row.properties["data-release-panel"]), ["v1.0.0"]);

  assert.equal(find(beta, (node) => node.tagName === "h2").properties.id, "beta-archive");
  const rows = beta.children[2].children;
  assert.deepEqual(rows.map((row) => [row.tagName, row.properties.name, row.properties["data-release-panel"]]),
    [["details", "release-history", "v1.0.0-rc.1"], ["details", "release-history", "v0.8.2"]]);
  assert.equal(find(rows[1], (node) => node.tagName === "h3").properties.id, "v082--order-of-arrival--beta--2026-10-02");
  assert.ok(rows.every((row) => !row.properties.open), "archive rows start closed");

  assert.equal(text(verifying), "Verifying a release");
  assert.equal(text(instructions), "Instructions");
});

test("before a stable release the archive explains the pending state", () => {
  const tree = { children: [heading("v0.8.2 — “Beta” — Beta — 2026-10-02"), paragraph("Beta notes")] };
  rehypeReleaseBrowser()(tree, { path: "/docs/site/releases/changelog.md" });
  assert.match(text(tree.children[0]), /first stable release is being prepared/);
  assert.equal(find(tree.children[1], (node) => node.tagName === "h2").properties.id, "beta-archive");
});

test("ordinary documentation pages are untouched", () => {
  const tree = { children: [heading("v1.0.0"), paragraph("Example")] };
  const before = structuredClone(tree);
  rehypeReleaseBrowser()(tree, { path: "/docs/site/start/install.md" });
  assert.deepEqual(tree, before);
});
