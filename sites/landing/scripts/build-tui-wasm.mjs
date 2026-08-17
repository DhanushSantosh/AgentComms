// Builds cmd/agent-comms-tui-wasm to sites/landing/public/tui/agent-comms-tui.wasm
// and copies the matching wasm_exec.js glue from the active Go toolchain
// alongside it. Runs as the first step of `npm run build` so the WASM
// binary and its loader are always fresh, real build artifacts -- never
// checked into git (see sites/landing/public/tui/ -- only wasm-bridge.js,
// Task 4's hand-written JS bridge, lives there in source control).
//
// Also bundles @xterm/xterm and @xterm/addon-fit into real ES modules under
// public/tui/vendor/. wasm-bridge.js resolves those packages as bare
// specifiers via a native browser `import()` at runtime, which only works
// against actual ES modules -- the npm packages themselves ship
// CommonJS/UMD only. An import map in src/app/layout.tsx points the bare
// specifiers at these bundles.
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import * as esbuild from "esbuild";

const repoRoot = resolve(import.meta.dirname, "..", "..", "..");
const outDir = resolve(import.meta.dirname, "..", "public", "tui");
mkdirSync(outDir, { recursive: true });

const wasmOut = resolve(outDir, "agent-comms-tui.wasm");
execFileSync("go", ["build", "-o", wasmOut, "./cmd/agent-comms-tui-wasm"], {
  cwd: repoRoot,
  env: { ...process.env, GOOS: "js", GOARCH: "wasm" },
  stdio: "inherit"
});

const goroot = execFileSync("go", ["env", "GOROOT"], { encoding: "utf8" }).trim();
const wasmExecCandidates = [
  resolve(goroot, "lib", "wasm", "wasm_exec.js"),
  resolve(goroot, "misc", "wasm", "wasm_exec.js")
];
const wasmExecSrc = wasmExecCandidates.find(existsSync);
if (!wasmExecSrc) {
  throw new Error(`wasm_exec.js not found in either of: ${wasmExecCandidates.join(", ")}`);
}
copyFileSync(wasmExecSrc, resolve(outDir, "wasm_exec.js"));
console.log(`Built agent-comms-tui.wasm -> ${wasmOut}`);

const vendorOutDir = resolve(outDir, "vendor");
mkdirSync(vendorOutDir, { recursive: true });
const vendorEntries = resolve(import.meta.dirname, "vendor-entries");
await esbuild.build({
  entryPoints: [
    resolve(vendorEntries, "xterm.js"),
    resolve(vendorEntries, "addon-fit.js")
  ],
  outdir: vendorOutDir,
  bundle: true,
  format: "esm",
  platform: "browser",
  minify: true,
  absWorkingDir: resolve(import.meta.dirname, ".."),
  logLevel: "info"
});
console.log(`Bundled xterm.js vendor ESM -> ${vendorOutDir}`);
