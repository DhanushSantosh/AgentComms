import { spawn } from "node:child_process";
import { mkdir, readFile } from "node:fs/promises";
import { resolve } from "node:path";

const host = "127.0.0.1";
const port = 4332;
const origin = `http://${host}:${port}`;
const outputDirectory = resolve("output/lighthouse");
const categories = ["performance", "accessibility", "best-practices", "seo"];
const minimumScore = 0.95;
const serverStartupTimeoutMilliseconds = 20_000;
const serverPollIntervalMilliseconds = 200;

await mkdir(outputDirectory, { recursive: true });
const preview = spawn(process.execPath, ["scripts/serve.mjs", "--host", host, "--port", String(port)], {
  stdio: "ignore",
  detached: process.platform !== "win32"
});

try {
  await waitForServer(origin);
  const outputPath = resolve(outputDirectory, "home.json");
  await run("npx", [
    "lighthouse", origin,
    "--quiet",
    "--chrome-flags=--headless=new --no-sandbox",
    `--only-categories=${categories.join(",")}`,
    "--output=json",
    `--output-path=${outputPath}`
  ]);
  const report = JSON.parse(await readFile(outputPath, "utf8"));
  const failures = categories.filter((category) => report.categories[category].score < minimumScore);
  const scores = Object.fromEntries(categories.map((category) => [category, report.categories[category].score]));
  console.log("Landing page Lighthouse scores:", scores);
  if (failures.length > 0) {
    throw new Error(`Landing page has Lighthouse categories below ${minimumScore}: ${failures.join(", ")}`);
  }
} finally {
  if (process.platform === "win32") preview.kill();
  else process.kill(-preview.pid, "SIGTERM");
}

async function waitForServer(url) {
  const deadline = Date.now() + serverStartupTimeoutMilliseconds;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch {
      // The local server is still starting.
    }
    await new Promise((resolvePromise) => setTimeout(resolvePromise, serverPollIntervalMilliseconds));
  }
  throw new Error(`Landing preview did not become ready at ${url}`);
}

function run(command, arguments_) {
  return new Promise((resolvePromise, rejectPromise) => {
    const processHandle = spawn(command, arguments_, { stdio: "inherit" });
    processHandle.on("error", rejectPromise);
    processHandle.on("exit", (code) => {
      if (code === 0) resolvePromise();
      else rejectPromise(new Error(`${command} exited with status ${code}`));
    });
  });
}
