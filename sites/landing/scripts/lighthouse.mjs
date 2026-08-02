import { spawn } from "node:child_process";
import { mkdir, readFile } from "node:fs/promises";
import { resolve } from "node:path";

const host = "127.0.0.1";
const port = 4332;
const origin = `http://${host}:${port}`;
const outputDirectory = resolve("output/lighthouse");
const categories = ["performance", "accessibility", "best-practices", "seo"];
// "performance" is timing-based and genuinely noisy even on desktop preset
// with a shared/busy CI runner -- a single bad scheduling slice can tank a
// run well below what the deployed, CDN-served site actually delivers. The
// other three categories are structural (markup/contrast/headers), not
// timing-sensitive, so they stay at the strict bar.
const minimumScoreByCategory = { performance: 0.8, accessibility: 0.95, "best-practices": 0.95, seo: 0.95 };
const serverStartupTimeoutMilliseconds = 20_000;
const serverPollIntervalMilliseconds = 200;
const runsPerPage = 3;

await mkdir(outputDirectory, { recursive: true });
const preview = spawn(process.execPath, ["scripts/serve.mjs", "--host", host, "--port", String(port)], {
  stdio: "ignore",
  detached: process.platform !== "win32"
});

try {
  await waitForServer(origin);
  const runs = [];
  for (let index = 0; index < runsPerPage; index += 1) {
    const outputPath = resolve(outputDirectory, `home-run${index}.json`);
    await run("npx", [
      "lighthouse", origin,
      "--quiet",
      "--preset=desktop",
      "--chrome-flags=--headless=new --no-sandbox",
      `--only-categories=${categories.join(",")}`,
      "--output=json",
      `--output-path=${outputPath}`
    ]);
    const report = JSON.parse(await readFile(outputPath, "utf8"));
    runs.push(Object.fromEntries(categories.map((category) => [category, report.categories[category].score])));
  }
  const medianScores = Object.fromEntries(categories.map((category) => [category, median(runs.map((r) => r[category]))]));
  const failures = categories.filter((category) => medianScores[category] < minimumScoreByCategory[category]);
  console.log(`Landing page Lighthouse runs (median of ${runsPerPage}):`, runs, "->", medianScores);
  if (failures.length > 0) {
    throw new Error(`Landing page has median Lighthouse categories below their minimum: ${failures.map((c) => `${c} (${medianScores[c]} < ${minimumScoreByCategory[c]})`).join(", ")}`);
  }
} finally {
  if (process.platform === "win32") preview.kill();
  else process.kill(-preview.pid, "SIGTERM");
}

function median(values) {
  const sorted = [...values].sort((a, b) => a - b);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 0 ? (sorted[middle - 1] + sorted[middle]) / 2 : sorted[middle];
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
