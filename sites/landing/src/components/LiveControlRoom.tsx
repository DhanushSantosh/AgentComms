"use client";

import { useEffect, useRef, useState } from "react";
import { motion } from "framer-motion";
// xterm.js needs its own stylesheet for correct cursor/row/viewport
// rendering. This is a normal build-time import resolved through webpack
// against the real @xterm/xterm npm package -- unrelated to the runtime
// import-map wiring below, which is only for wasm-bridge.js's own bare
// specifier imports.
import "@xterm/xterm/css/xterm.css";
import { installVirtualFs } from "@/lib/wasm-node-fs";

// Launches a real, live instance of the product's TUI, compiled to WASM
// (Task 4's cmd/agent-comms-tui-wasm + public/tui/wasm-bridge.js), in place
// of a centered launch button. Clicking plays a framer-motion scale/opacity
// "expand" on an empty box first ("expanding"), and only once that
// animation genuinely finishes does the terminal actually mount and start
// booting WASM inside it ("loading"). This ordering is required, not
// cosmetic: xterm's FitAddon measures the container via
// getBoundingClientRect(), which -- unlike clientWidth/clientHeight --
// DOES reflect an in-flight CSS transform. Starting the WASM boot (and
// FitAddon.fit() inside it) while the box is still mid-scale measures a
// shrunken size and bakes a too-narrow column count into the whole
// session, which nothing later corrects (confirmed empirically: animating
// the terminal container itself, rather than sequencing around the
// animation, reproduces a permanent narrow-column-wrapped render even
// though the container's own final CSS dimensions are correct throughout).
type LaunchState = "idle" | "expanding" | "loading" | "live" | "error";

export function LiveControlRoom() {
  const containerRef = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<LaunchState>("idle");

  useEffect(() => {
    if (state !== "loading" || !containerRef.current) return;
    let cancelled = false;
    (async () => {
      try {
        // wasm_exec.js is a plain (non-module) IIFE that assigns
        // `globalThis.Go`; importing it purely for that side effect works
        // because a script with no import/export statements is also a
        // valid ES module. These paths are runtime-only static files
        // produced by scripts/build-tui-wasm.mjs and Task 4's
        // wasm-bridge.js -- routed through a variable (rather than a
        // string literal) so neither TypeScript nor webpack try to
        // resolve/bundle them as build-time modules; both just leave a
        // native, unbundled `import()` for the browser to run.
        // Must run before wasm_exec.js: it only installs its own
        // ENOSYS-everything `globalThis.fs` stub when one doesn't already
        // exist, so installing a real in-memory one first is what lets
        // cmd/agent-comms-tui-wasm's ordinary os.MkdirTemp/WriteFile/
        // ReadFile calls succeed under GOOS=js instead of failing with
        // "not implemented on js".
        installVirtualFs();
        const wasmExecUrl = "/tui/wasm_exec.js";
        await import(/* webpackIgnore: true */ wasmExecUrl);
        const wasmBridgeUrl = "/tui/wasm-bridge.js";
        const { launchAgentCommsTUI } = (await import(
          /* webpackIgnore: true */ wasmBridgeUrl
        )) as { launchAgentCommsTUI: (container: HTMLElement) => Promise<void> };
        if (cancelled || !containerRef.current) return;
        await launchAgentCommsTUI(containerRef.current);
        if (!cancelled) setState("live");
      } catch {
        if (!cancelled) setState("error");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [state]);

  if (state === "idle") {
    return (
      <div className="control-launcher">
        <button type="button" className="action action--ink control-launch" onClick={() => setState("expanding")}>
          Launch the Control Room <span>↗</span>
        </button>
      </div>
    );
  }

  // "expanding" plays the entrance animation against an empty box -- no
  // containerRef, no WASM boot -- so nothing ever measures the box while
  // its transform is mid-flight. onAnimationComplete is what actually
  // advances to "loading"; it does not re-fire on the later
  // loading->live/error transitions below because animate's target values
  // ({ opacity: 1, scale: 1 }) don't change again after this first run.
  return (
    <motion.div
      className="control-live"
      initial={state === "expanding" ? { opacity: 0, scale: 0.5 } : false}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ duration: 0.6, ease: [0.16, 1, 0.3, 1] }}
      onAnimationComplete={() => {
        if (state === "expanding") setState("loading");
      }}
      aria-live="polite"
    >
      {state === "loading" && <p className="control-loading">Starting the real TUI…</p>}
      {state !== "expanding" && <div ref={containerRef} className="control-terminal" />}
      {state === "error" && (
        <p className="control-launch-error">
          Couldn&rsquo;t load the live terminal.{" "}
          <button type="button" onClick={() => setState("loading")}>
            Try again
          </button>
        </p>
      )}
      {state === "live" && (
        <button
          type="button"
          className="action action--line control-reset"
          onClick={() => window.location.reload()}
        >
          Reset session <span>↻</span>
        </button>
      )}
    </motion.div>
  );
}
