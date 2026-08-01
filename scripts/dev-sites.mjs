import { spawn } from "node:child_process";

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
const landingPort = readPort("LANDING_DEV_PORT", defaultLandingPort);
const documentationPort = readPort("DOCS_DEV_PORT", defaultDocumentationPort);
const landingUrl = `http://${localHost}:${landingPort}`;
const documentationUrl = `http://${localHost}:${documentationPort}`;
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
    const outcome = signal ? `signal ${signal}` : `exit code ${code ?? 1}`;
    console.error(`A site development process stopped with ${outcome}; stopping its peer.`);
    process.exitCode = code === 0 ? 1 : code ?? 1;
    stopChildren("SIGTERM");
  });
}

process.on("SIGINT", () => stopChildren("SIGINT"));
process.on("SIGTERM", () => stopChildren("SIGTERM"));
