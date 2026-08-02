import { spawn } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, resolve as resolvePath } from "node:path";
import { fileURLToPath } from "node:url";

loadLocalEnvFile();

const defaultLocalHost = "127.0.0.1";
const defaultLandingPort = 3_000;
const defaultDocumentationPort = 4_321;

function readPort(environmentName, fallback) {
  const value = process.env[environmentName];
  if (!value) return fallback;
  const port = Number(value);
  if (!Number.isInteger(port) || port < 1 || port > 65_535) {
    throw new Error(`${environmentName} must be an integer from 1 through 65535.`);
  }
  return port;
}

const localHost = process.env.SITES_DEV_HOST ?? defaultLocalHost;
// Astro's dev CLI crashes (`new URL(server.resolvedUrls.local[0]).origin`,
// node_modules/astro/dist/cli/dev/index.js) when --host is a specific LAN
// IP rather than 0.0.0.0/localhost, because Vite only populates
// resolvedUrls.local for loopback-style hosts. Binding to a LAN IP to test
// from another device on the network therefore needs 0.0.0.0 as the bind
// address while the cross-site links (NEXT_PUBLIC_DOCS_URL etc.) still
// need the real LAN IP, or a phone/other device can't resolve them --
// SITES_PUBLIC_HOST lets those two concerns differ; it defaults to
// localHost so existing callers that only set SITES_DEV_HOST are unaffected.
const publicHost = process.env.SITES_PUBLIC_HOST ?? localHost;
const landingPort = readPort("LANDING_DEV_PORT", defaultLandingPort);
const documentationPort = readPort("DOCS_DEV_PORT", defaultDocumentationPort);
const landingUrl = `http://${publicHost}:${landingPort}`;
const documentationUrl = `http://${publicHost}:${documentationPort}`;
const npmCommand = process.platform === "win32" ? "npm.cmd" : "npm";

const sharedOptions = {
  shell: false,
  stdio: "inherit"
};

const landing = spawn(
  npmCommand,
  ["run", "dev", "--workspace", "@agent-comms/landing", "--", "--hostname", localHost, "--port", String(landingPort)],
  {
    ...sharedOptions,
    env: {
      ...process.env,
      NEXT_PUBLIC_DOCS_URL: documentationUrl,
      NEXT_PUBLIC_SITE_URL: landingUrl
    }
  }
);

const documentation = spawn(
  npmCommand,
  ["run", "dev", "--workspace", "@agent-comms/docs", "--", "--host", localHost, "--port", String(documentationPort)],
  {
    ...sharedOptions,
    env: {
      ...process.env,
      DOCS_SITE_URL: documentationUrl,
      PUBLIC_MARKETING_SITE_URL: landingUrl
    }
  }
);

const children = [landing, documentation];
let shuttingDown = false;

function stopChildren(signal) {
  if (shuttingDown) return;
  shuttingDown = true;
  for (const child of children) {
    if (!child.killed) child.kill(signal);
  }
}

for (const child of children) {
  child.on("error", (error) => {
    console.error(`Unable to start site development process: ${error.message}`);
    stopChildren("SIGTERM");
    process.exitCode = 1;
  });
  child.on("exit", (code, signal) => {
    if (shuttingDown) return;
    // A clean, unsignaled exit(0) isn't a crash to treat as fatal for the
    // peer -- Astro's dev CLI (v7+) hands off to a persistent background
    // daemon and its launching process then exits 0 once that handoff
    // succeeds (the daemon keeps listening independently). Only a nonzero
    // exit or a signal indicates the process actually failed.
    if (code === 0 && !signal) return;
    const outcome = signal ? `signal ${signal}` : `exit code ${code ?? 1}`;
    console.error(`A site development process stopped with ${outcome}; stopping its peer.`);
    process.exitCode = code ?? 1;
    stopChildren("SIGTERM");
  });
}

process.on("SIGINT", () => stopChildren("SIGINT"));
process.on("SIGTERM", () => stopChildren("SIGTERM"));

// Untracked, per-machine overrides (e.g. this device's Tailscale IP for
// SITES_PUBLIC_HOST so other devices on the tailnet can reach the dev
// servers) -- silently absent for anyone who hasn't created one.
function loadLocalEnvFile() {
  const repoRoot = resolvePath(dirname(fileURLToPath(import.meta.url)), "..");
  let contents;
  try {
    contents = readFileSync(resolvePath(repoRoot, ".env.local"), "utf8");
  } catch {
    return;
  }
  for (const line of contents.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const separatorIndex = trimmed.indexOf("=");
    if (separatorIndex === -1) continue;
    const key = trimmed.slice(0, separatorIndex).trim();
    const value = trimmed.slice(separatorIndex + 1).trim();
    if (process.env[key] === undefined) process.env[key] = value;
  }
}
