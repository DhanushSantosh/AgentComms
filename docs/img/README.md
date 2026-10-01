# Product media

Captured on 2026-10-01 from `dev` based on `d44140d` (v0.8.1 plus its three
post-release follow-ups). The assets show the actual shared `internal/tui`
renderer. The data is a synthetic, isolated project created through the same
governed seed transitions used by the landing site's interactive WASM demo.
They do not show the owner's working project or credentials.

- `tui-overview.png`: messaging readiness, runtime presence, attention, work,
  and recent signed events.
- `tui-agents.png`: lifecycle, roles, principal types, and scopes.
- `tui-approvals.png`: pending human-tier approval inspection.
- `tui-demo.mp4` and `tui-demo.gif`: a silent terminal navigation recording;
  waiting pauses are shortened. No transport delivery or completed test run
  is staged as proof.

The README uses a still image so reading it does not require continuous
animation. The landing mobile fallback uses the still as a poster and plays
the MP4 only on request. The docs link the MP4 and GIF explicitly.

## Refresh

Build the isolated native demo (the JS/WASM entrypoint is unchanged):

```sh
go build -o /tmp/agent-comms-site-demo ./cmd/agent-comms-tui-wasm
ttyd -i 127.0.0.1 -p 7681 -O -m 1 -W -t fontSize=16 /tmp/agent-comms-site-demo
```

`ttyd` is optional capture tooling, not a product or installation dependency.
It must bind only to loopback. Open that local page and capture the terminal,
not a user's live project. Useful keys: `g` for Agents, `i` for Invocations,
Escape to leave row focus, `o` for Overview, and three Right presses from
Overview for Approvals. Enter focuses its table; `i` opens its inspector.
Do not approve or complete anything just to make a marketing claim.

Use the browser's PNG screenshot and screencast APIs. Retain frame timestamps
while recording, acknowledge screencast frames, and stop the recorder when
finished. Encode a silent H.264 MP4 with `yuv420p` and `+faststart`; generate
the GIF with a palette pass. Review the PNGs and decoded video before replacing
files. Copy identical public assets to `sites/docs/public/`, and the overview
PNG and MP4 to `sites/landing/public/media/`. Run both site checks and media
tests after a refresh. Stop the loopback capture server afterwards.
