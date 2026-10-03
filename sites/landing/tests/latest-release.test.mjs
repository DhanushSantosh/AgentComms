import test from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { createRequire } from "node:module";
import { build } from "esbuild";

const require = createRequire(import.meta.url);
const bundle = await build({
  entryPoints: [new URL("../src/lib/latestRelease.ts", import.meta.url).pathname],
  bundle: true, platform: "node", format: "cjs", write: false
});
const source = bundle.outputFiles[0].text;

function fixture(status, body) {
  let options;
  let calls = 0;
  const fakeHttps = { get(_url, config, callback) {
    options = config;
    calls++;
    const request = new EventEmitter();
    request.setTimeout = () => request;
    queueMicrotask(() => {
      const response = new EventEmitter();
      response.statusCode = status;
      response.resume = () => {};
      response.setEncoding = () => {};
      callback(response);
      response.emit("data", body);
      response.emit("end");
    });
    return request;
  } };
  const module = { exports: {} };
  new Function("require", "module", "exports", source)(
    (name) => name === "node:https" ? fakeHttps : require(name), module, module.exports
  );
  return { getLatestVersion: module.exports.getLatestVersion, headers: () => options.headers, calls: () => calls };
}

test("release lookup uses a server-only token and shares one request", async () => {
  const previous = process.env.AGENT_COMMS_RELEASE_API_TOKEN;
  process.env.AGENT_COMMS_RELEASE_API_TOKEN = "synthetic-build-token";
  try {
    const lookup = fixture(200, '{"tag_name":"v0.8.2"}');
    assert.deepEqual(await Promise.all([lookup.getLatestVersion(), lookup.getLatestVersion()]), ["0.8.2", "0.8.2"]);
    assert.equal(lookup.headers().Authorization, "Bearer synthetic-build-token");
    assert.equal(lookup.calls(), 1);
  } finally {
    if (previous === undefined) delete process.env.AGENT_COMMS_RELEASE_API_TOKEN;
    else process.env.AGENT_COMMS_RELEASE_API_TOKEN = previous;
  }
});

test("anonymous builds omit authorization; bad responses fail closed", async () => {
  const previous = process.env.AGENT_COMMS_RELEASE_API_TOKEN;
  delete process.env.AGENT_COMMS_RELEASE_API_TOKEN;
  try {
    const lookup = fixture(200, '{"tag_name":"v0.8.2"}');
    assert.equal(await lookup.getLatestVersion(), "0.8.2");
    assert.equal(lookup.headers().Authorization, undefined);
    await assert.rejects(fixture(403, "denied").getLatestVersion(), /returned 403/);
    await assert.rejects(fixture(200, "not-json").getLatestVersion(), SyntaxError);
    await assert.rejects(fixture(200, '{"tag_name":"not-a-version"}').getLatestVersion(), /invalid release tag/);
  } finally {
    if (previous !== undefined) process.env.AGENT_COMMS_RELEASE_API_TOKEN = previous;
  }
});
