// Re-exports @xterm/xterm's UMD build as a named ESM export. wasm-bridge.js
// (Task 4) resolves "@xterm/xterm" as a bare specifier via a native browser
// `import()`, which only works against a real ES module -- not the raw npm
// package, which ships CommonJS/UMD only. This tiny entry point is bundled
// by scripts/build-tui-wasm.mjs into public/tui/vendor/xterm.js, and an
// import map (see src/app/layout.tsx) points the bare specifier at that
// bundle.
import pkg from "@xterm/xterm";

export const Terminal = pkg.Terminal;
