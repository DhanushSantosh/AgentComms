import assert from "node:assert/strict";
import test from "node:test";
import CachePolicy from "http-cache-semantics";

// Exercise the installed transitive dependency's public API. These synthetic
// shared-cache fixtures caught GHSA-ch52-4w7c-c8xp on 4.2.0; do not weaken the
// repository-wide npm audit gate when a future upstream advisory appears.
const request = { url: "https://fixture.invalid/content", method: "GET", headers: {} };
const maxStaleRequest = { ...request, headers: { "cache-control": "max-stale=86400" } };

for (const [name, headers] of [
  ["private session cookie", { "cache-control": "max-age=3600", "set-cookie": "session=synthetic" }],
  ["proxy revalidation", { "cache-control": "max-age=0, proxy-revalidate" }],
  ["mandatory revalidation", { "cache-control": "max-age=0, must-revalidate" }],
  ["no-cache", { "cache-control": "no-cache" }],
  ["no-store", { "cache-control": "no-store, max-age=3600" }],
  ["private response", { "cache-control": "private, max-age=3600" }],
]) {
  test(`max-stale cannot override ${name}`, () => {
    const policy = new CachePolicy(request, { status: 200, headers });
    assert.equal(policy.satisfiesWithoutRevalidation(maxStaleRequest), false);
  });
}

test("ordinary public cached content remains reusable", () => {
  const policy = new CachePolicy(request, {
    status: 200,
    headers: { "cache-control": "public, max-age=3600" },
  });
  assert.equal(policy.storable(), true);
  assert.equal(policy.satisfiesWithoutRevalidation(request), true);
  assert.ok(policy.timeToLive() > 0);
});

test("explicitly public cookies retain the documented opt-in", () => {
  const policy = new CachePolicy(request, {
    status: 200,
    headers: { "cache-control": "public, max-age=3600", "set-cookie": "public=synthetic" },
  });
  assert.equal(policy.satisfiesWithoutRevalidation(request), true);
});

test("ordinary public expired responses may still honor max-stale", () => {
  const policy = new CachePolicy(request, { status: 200, headers: { "cache-control": "public, max-age=0" } });
  assert.equal(policy.satisfiesWithoutRevalidation(maxStaleRequest), true);
});

test("immutable cookies retain the documented opt-in", () => {
  const policy = new CachePolicy(request, {
    status: 200,
    headers: { "cache-control": "immutable, max-age=3600", "set-cookie": "immutable=synthetic" },
  });
  assert.equal(policy.satisfiesWithoutRevalidation(request), true);
});

test("private caches may honor proxy-revalidate because it constrains shared caches", () => {
  const policy = new CachePolicy(request, { status: 200, headers: { "cache-control": "max-age=0, proxy-revalidate" } }, { shared: false });
  assert.equal(policy.satisfiesWithoutRevalidation(maxStaleRequest), true);
});
