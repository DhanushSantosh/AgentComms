//go:build js

package projectlifecycle

import "github.com/DhanushSantosh/AgentComms/internal/store"

// js/wasm entry points for the lifecycle operations that packages in the
// WASM demo's dependency graph reach for. They cannot be carried out inside
// a browser sandbox -- there is no on-disk project and no SQLite database to
// read a schema version from -- so they report that plainly instead of
// pretending to have done the work.
//
// Deliberately NOT provided here: Reconcile, and the rest of lifecycle.go's
// surface. Nothing in the js graph calls them, and a stub that silently
// "succeeded" at upgrading a project would be a far worse failure mode than
// a build error naming the exact call that cannot work.

// unavailableMessage is the single explanation every entry point here gives.
const unavailableMessage = "project lifecycle inspection is unavailable in js/wasm builds: " +
	"there is no on-disk project or SQLite database to inspect"

// Inspect reports that inspection is unavailable. The returned Plan echoes
// only what the caller already told us (the root it asked about and the
// build ID it is running), never an invented verdict: it lists no actions
// because none were computed, not because the project was found current.
// Callers that ignore the error -- internal/tui does -- must not read the
// empty action list as "up to date"; the TUI's js build renders lifecycle
// compatibility as unavailable rather than CURRENT for exactly this reason.
func Inspect(root, version, buildID string) (Plan, store.Config, error) {
	_ = version
	return Plan{ProjectRoot: root, TargetBuildID: buildID}, store.Config{}, &Error{
		Code:    CodeUpgradeUnsupported,
		Message: unavailableMessage,
	}
}
