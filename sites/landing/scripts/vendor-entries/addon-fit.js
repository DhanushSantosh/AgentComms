// See xterm.js in this directory for why this wrapper exists: it turns
// @xterm/addon-fit's CommonJS/UMD build into a real ES module with a named
// `FitAddon` export, so the import map in src/app/layout.tsx can satisfy
// wasm-bridge.js's `import("@xterm/addon-fit")` with a real ESM file.
import pkg from "@xterm/addon-fit";

export const FitAddon = pkg.FitAddon;
