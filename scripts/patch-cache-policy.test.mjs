import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { applyCachePolicyPatch, insertion, upstreamHash, patchedHash } from "./patch-cache-policy.mjs";

const require = createRequire(import.meta.url);
const installed = readFileSync(require.resolve("http-cache-semantics"), "utf8");
const original = installed.replace(insertion, "");
const hash = (text) => createHash("sha256").update(text).digest("hex");
assert.equal(hash(original), upstreamHash, "maintenance fixture must be the reviewed upstream source");

function fixture(t, { version = "4.3.0", source = original } = {}) {
  const root = mkdtempSync(path.join(os.tmpdir(), "agc-cache-policy-test-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  writeFileSync(path.join(root, "package.json"), "{}");
  const dependency = path.join(root, "node_modules", "http-cache-semantics");
  mkdirSync(dependency, { recursive: true });
  writeFileSync(path.join(dependency, "package.json"), JSON.stringify({ name: "http-cache-semantics", version, main: "index.js" }));
  const target = path.join(dependency, "index.js");
  writeFileSync(target, source);
  const docs = path.join(root, "sites", "docs");
  const astro = path.join(docs, "node_modules", "astro");
  mkdirSync(astro, { recursive: true });
  writeFileSync(path.join(docs, "package.json"), "{}");
  writeFileSync(path.join(astro, "package.json"), JSON.stringify({ name: "astro", main: "index.js" }));
  writeFileSync(path.join(astro, "index.js"), "// Synthetic consumer resolution fixture\n");
  return { root, target, dependency };
}

test("pristine input is patched atomically and repeated installation is idempotent", (t) => {
  const { root, target } = fixture(t);
  assert.equal(applyCachePolicyPatch(root), "patched");
  assert.equal(hash(readFileSync(target, "utf8")), patchedHash);
  assert.equal(applyCachePolicyPatch(root), "already patched");
  assert.equal(hash(readFileSync(target, "utf8")), patchedHash);
});

for (const version of ["4.2.0", "4.3.1", "5.0.0"]) {
  test(`unknown version ${version} is refused without changing source`, (t) => {
    const { root, target } = fixture(t, { version });
    assert.throws(() => applyCachePolicyPatch(root), /Unknown cache-policy version/);
    assert.equal(readFileSync(target, "utf8"), original);
  });
}

test("modified source and incomplete prior writes are refused unchanged", (t) => {
  for (const source of [`${original}\n// unexpected input`, original.slice(0, 100)]) {
    const { root, target } = fixture(t, { source });
    assert.throws(() => applyCachePolicyPatch(root), /Unknown cache-policy source hash/);
    assert.equal(readFileSync(target, "utf8"), source);
  }
});

test("missing dependency fails with an installation diagnostic", (t) => {
  const { root, dependency } = fixture(t);
  rmSync(dependency, { recursive: true });
  assert.throws(() => applyCachePolicyPatch(root), /install all workspace dependencies/);
});

test("a different dependency copy resolved by Astro is refused", (t) => {
  const { root, target } = fixture(t);
  const nested = path.join(root, "sites", "docs", "node_modules", "astro", "node_modules", "http-cache-semantics");
  mkdirSync(nested, { recursive: true });
  writeFileSync(path.join(nested, "package.json"), JSON.stringify({ name: "http-cache-semantics", version: "4.3.0", main: "index.js" }));
  writeFileSync(path.join(nested, "index.js"), original);
  assert.throws(() => applyCachePolicyPatch(root), /another cache-policy copy/);
  assert.equal(readFileSync(target, "utf8"), original);
  assert.equal(readFileSync(path.join(nested, "index.js"), "utf8"), original);
});

test("a resolved dependency outside workspace node_modules is never modified", { skip: process.platform === "win32" }, (t) => {
  const local = fixture(t);
  const external = fixture(t);
  rmSync(local.dependency, { recursive: true });
  symlinkSync(external.dependency, local.dependency, "dir");
  assert.throws(() => applyCachePolicyPatch(local.root), /install all workspace dependencies/);
  assert.equal(readFileSync(external.target, "utf8"), original);
});

test("a manifest symlink outside workspace is refused", { skip: process.platform === "win32" }, (t) => {
  const local = fixture(t);
  const external = fixture(t);
  const manifest = path.join(local.dependency, "package.json");
  rmSync(manifest);
  symlinkSync(path.join(external.dependency, "package.json"), manifest);
  assert.throws(() => applyCachePolicyPatch(local.root), /inside this workspace/);
  assert.equal(readFileSync(local.target, "utf8"), original);
});
