import assert from "node:assert/strict";
import test from "node:test";
import rehypeReleaseBrowser from "./rehype-release-browser.mjs";

const heading = (title) => ({ type: "element", tagName: "h2", properties: {}, children: [{ type: "text", value: title }] });
const paragraph = (value) => ({ type: "element", tagName: "p", properties: {}, children: [{ type: "text", value }] });

test("stable and prerelease history stay separate, with one initial panel per group", () => {
  const tree = { children: [paragraph("Introduction"), heading("v1.1.0 — Second"), paragraph("New stable"),
    heading("v1.0.0 — First"), paragraph("First stable"), heading("v1.0.0-rc.1 — Candidate"), paragraph("Candidate notes"),
    heading("v0.8.2 — Beta"), paragraph("Beta notes"), heading("Verifying a release"), paragraph("Instructions")] };
  rehypeReleaseBrowser()(tree, { path: "/docs/site/releases/changelog.md" });
  const stable = tree.children[1];
  const beta = tree.children[2];
  assert.equal(beta.tagName, "details");
  assert.equal(beta.properties.id, "beta-archive");
  assert.deepEqual(stable.children.slice(1).map((panel) => [panel.properties["data-release-panel"], panel.properties.hidden]), [["v1.1.0", false], ["v1.0.0", true]]);
  assert.deepEqual(beta.children.slice(3).map((panel) => panel.properties["data-release-panel"]), ["v1.0.0-rc.1", "v0.8.2"]);
  assert.equal(tree.children[3].children[0].value, "Verifying a release");
  assert.equal(tree.children[4].children[0].value, "Instructions");
});

test("ordinary documentation pages are untouched", () => {
  const tree = { children: [heading("v1.0.0"), paragraph("Example")] };
  const before = structuredClone(tree);
  rehypeReleaseBrowser()(tree, { path: "/docs/site/start/install.md" });
  assert.deepEqual(tree, before);
});
