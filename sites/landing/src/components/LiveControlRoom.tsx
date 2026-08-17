"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
// xterm.js needs its own stylesheet for correct cursor/row/viewport
// rendering. This is a normal build-time import resolved through webpack
// against the real @xterm/xterm npm package -- unrelated to the runtime
// import-map wiring below, which is only for wasm-bridge.js's own bare
// specifier imports.
import "@xterm/xterm/css/xterm.css";
import { installVirtualFs } from "@/lib/wasm-node-fs";

// Swaps the static ControlRoomFrame poster for a real, live instance of the
// product's TUI, compiled to WASM (Task 4's cmd/agent-comms-tui-wasm +
// public/tui/wasm-bridge.js). State lives here rather than being split
// across the poster and this component, so exactly one of "the poster" or
// "the live terminal" is ever mounted -- no CSS overlay stacking two copies
// of the chrome at once.
type LaunchState = "idle" | "loading" | "live" | "error";

export function LiveControlRoom({ poster }: { poster: ReactNode }) {
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
        {poster}
        <button type="button" className="action action--ink control-launch" onClick={() => setState("loading")}>
          Launch the Control Room <span>↗</span>
        </button>
      </div>
    );
  }

  if (state === "error") {
    return (
      <div className="control-launcher">
        {poster}
        <p className="control-launch-error">
          Couldn&rsquo;t load the live terminal.{" "}
          <button type="button" onClick={() => setState("loading")}>
            Try again
          </button>
        </p>
      </div>
    );
  }

  return (
    <div className="control-live" aria-live="polite">
      {state === "loading" && <p className="control-loading">Starting the real TUI…</p>}
      <div ref={containerRef} className="control-terminal" />
      {state === "live" && (
        <button
          type="button"
          className="action action--line control-reset"
          onClick={() => window.location.reload()}
        >
          Reset session <span>↻</span>
        </button>
      )}
    </div>
  );
}
