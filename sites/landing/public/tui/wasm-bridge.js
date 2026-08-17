// Thin JS glue between xterm.js and the agent-comms-tui-wasm Go program's
// syscall/js exports (see cmd/agent-comms-tui-wasm/jsbridge.go).
//
// launchAgentCommsTUI(container) mounts a themed xterm.js Terminal into
// `container`, wires its keystrokes/resizes to the WASM program's exported
// agentCommsTUIWrite/agentCommsTUIResize functions, installs the
// window.agentCommsTUIOnOutput callback the WASM program writes terminal
// output through, then loads and starts the WASM module itself.
//
// Two things this file assumes are already true by the time it runs (both
// wired by a later task, not this one):
//   - `@xterm/xterm` and `@xterm/addon-fit` are real npm dependencies of
//     sites/landing (they are not yet -- see that task's notes).
//   - `wasm_exec.js` has already run as a plain <script> tag, so the global
//     `Go` constructor it defines exists.
//
// Palette: the exact five accent colors + background/foreground this project
// uses everywhere else, read directly from internal/tui/model.go's colors()
// function (the single source of truth for TUI colors) rather than
// approximated:
//   ink (background)  #071216   text (foreground)  #D7E5E3
//   cyan               #56D6C9   amber               #E8B85C
//   coral (red)        #F07167   lilac (violet)      #B9A7E8
//   steel (muted)      #78918F
// xterm.js measures its own cell size with an OffscreenCanvas 2D context
// (`ctx.font = "<size>px <fontFamily>"`, then `ctx.measureText("W")`'s
// fontBoundingBoxAscent/Descent -- see @xterm/xterm's RendererMetrics /
// DomMeasureStrategy fallback chain). An OffscreenCanvas has no DOM
// cascade, so a raw CSS custom property like "var(--mono)" cannot resolve
// there: the browser silently rejects the `ctx.font` assignment and keeps
// its built-in default ("10px sans-serif"), which measures to an
// ascent+descent around 11px. Meanwhile the *rendered* row divs live in
// the real DOM, where "var(--mono)" resolves normally against whatever
// font (here, next/font's self-hosted commit-mono, ~15px tall glyphs at
// the default fontSize) actually loaded. That 11px-measured-cell vs.
// 15px-rendered-glyph mismatch is what makes adjacent terminal rows
// visually overlap/cramp -- not a font-metrics quirk of commit-mono
// itself. Resolving the custom property to a literal, already-cascaded
// font-family string before handing it to xterm keeps measurement and
// rendering using the exact same metrics.
function resolveFontFamily(container) {
  const probe = document.createElement("span");
  probe.style.fontFamily = "var(--mono)";
  probe.style.position = "absolute";
  probe.style.visibility = "hidden";
  container.appendChild(probe);
  const resolved = getComputedStyle(probe).fontFamily;
  container.removeChild(probe);
  return resolved;
}

export async function launchAgentCommsTUI(container) {
  const { Terminal } = await import("@xterm/xterm");
  const { FitAddon } = await import("@xterm/addon-fit");

  const term = new Terminal({
    theme: {
      background: "#071216",
      foreground: "#D7E5E3",
      cursor: "#56D6C9",
      black: "#071216",
      brightBlack: "#78918F",
      cyan: "#56D6C9",
      brightCyan: "#56D6C9",
      yellow: "#E8B85C",
      brightYellow: "#E8B85C",
      red: "#F07167",
      brightRed: "#F07167",
      magenta: "#B9A7E8",
      brightMagenta: "#B9A7E8",
      white: "#D7E5E3",
      brightWhite: "#D7E5E3",
    },
    fontFamily: resolveFontFamily(container),
    convertEol: true,
  });

  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(container);
  fit.fit();

  // Output side: the WASM program calls this on every write to its
  // io.Writer (jsOutputWriter in jsbridge.go).
  window.agentCommsTUIOnOutput = (text) => term.write(text);

  // Input side: every xterm.js keystroke goes straight to the WASM
  // program's input buffer as raw bytes.
  term.onData((data) => {
    window.agentCommsTUIWrite(new TextEncoder().encode(data));
  });

  // Every resize (including the initial fit() above, which fires onResize)
  // becomes a synthetic WindowSizeEvent in the same input stream.
  term.onResize(({ cols, rows }) => {
    window.agentCommsTUIResize(cols, rows);
  });

  const go = new Go(); // eslint-disable-line no-undef -- wasm_exec.js global
  const wasm = await WebAssembly.instantiateStreaming(
    fetch("/tui/agent-comms-tui.wasm"),
    go.importObject,
  );
  // go.run() does not resolve until the Go program exits, and this program
  // never exits (its main is `select {}` forever) -- don't await it inline,
  // just let it run in the background.
  go.run(wasm.instance);

  window.addEventListener("resize", () => fit.fit());

  return term;
}
