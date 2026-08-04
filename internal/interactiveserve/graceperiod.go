package interactiveserve

import "time"

// GracePeriod bounds how long a forwarded shutdown signal is given to let a
// process exit on its own before escalating to an outright kill. Shared,
// platform-independent constant: Serve (serve.go, !windows only) uses it for
// forwardAndWait, and Takeover (takeover.go/takeover_windows.go) uses it to
// wait the same amount of time for an existing session to exit cleanly
// before escalating. Declared without a build tag so callers like
// internal/app can reference it unconditionally regardless of GOOS.
const GracePeriod = 3 * time.Second
