// Package main is the WASM entrypoint for a real, live instance of the
// product TUI (internal/tui), running against an in-process daemon backed
// entirely by internal/wasmdemo's in-memory authority and cache -- no
// SQLite, no real OS socket, and (per wasm_main.go's build tag) safe under
// GOOS=js GOARCH=wasm.
//
// Bridging internal/daemonclient to the in-process internal/daemon.Handler:
// internal/daemonclient.Client is a plain net/http.Client under the hood
// (confirmed by reading its source, and by internal/service/remote_test.go's
// own TestNewWithRemoteConstructsUsableService, this package's closest real
// precedent), so the in-process bridge here is an http.RoundTripper
// (inProcessTransport, below) that calls daemon.Handler().ServeHTTP against
// an httptest.NewRecorder() and returns recorder.Result() -- no real
// network/socket involved at all, and identical under GOOS=js since it's
// pure Go with no networking syscalls. NewWithServer/net.Listener was never
// needed: daemonclient.NewWithTransport already accepts an injectable
// http.RoundTripper directly.
//
// Project bootstrap deliberately does NOT call internal/runtimeinit.Initialize,
// despite that being the pattern internal/testsupport.StartPersonalProject and
// remote_test.go use on the host: runtimeinit.Initialize's "personal" mode
// path unconditionally opens internal/personalauthority (modernc.org/sqlite),
// which does not compile for GOOS=js at all -- modernc.org/libc (sqlite's
// dependency) has zero source files satisfying js/wasm build constraints,
// confirmed directly with `GOOS=js GOARCH=wasm go build ./internal/runtimeinit`
// failing across every modernc.org/libc subpackage it imports (errno, limits,
// pthread, signal, stdio, sys/types, time, unistd). Since bootstrapDemoService
// must compile and run identically whether invoked from wasm_main.go's real
// GOOS=js entrypoint or from seed_test.go's ordinary host test, it cannot
// depend on runtimeinit at all. bootstrapProject below reproduces exactly the
// side effects this demo actually needs from runtimeinit.Initialize's
// "personal" branch -- a project ID, a store.Config on disk, an owner
// credential, and the owner's real agent.register + agent.activate(OWNER)
// bootstrap events -- adapting runtimeinit.go's own initialCommands() to call
// this demo's *wasmdemo.MemoryAuthority directly instead of
// personalauthority.Engine, and skipping every SQLite-backed step
// (personalauthority.Open, engine.CreateProject/Command) entirely in favor of
// the equivalent MemoryAuthority calls.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/daemon"
	"github.com/DhanushSantosh/AgentComms/internal/daemonclient"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/DhanushSantosh/AgentComms/internal/wasmdemo"
	"github.com/google/uuid"
)

// demoOwner is the human actor every seeded transition in seed.go ultimately
// traces its authority back to -- the TUI's own actor argument (tui.Run's
// second parameter) is this same value.
const demoOwner = "owner"

// inProcessTransport is the http.RoundTripper bridge described in this
// file's package comment: it never opens a socket, it calls handler
// in-process against an httptest.ResponseRecorder.
type inProcessTransport struct {
	handler http.Handler
}

func (t inProcessTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, request)
	return recorder.Result(), nil
}

// bootstrapDemoService wires an in-process daemon backed by the in-memory
// authority + cache from internal/wasmdemo, seeds a fresh demo project's
// on-disk store.Config and owner identity, and returns a real
// *service.Service pointed at it -- no SQLite, no real OS socket, safe under
// GOOS=js. Exported (lowercase, package-internal) so both seed_test.go (on
// the host) and wasm_main.go (under GOOS=js) can call the exact same code
// path.
func bootstrapDemoService() (*service.Service, error) {
	signer, err := controlplane.GenerateSigner()
	if err != nil {
		return nil, fmt.Errorf("generate demo signer: %w", err)
	}

	authority := wasmdemo.NewMemoryAuthority(signer)
	cache := wasmdemo.NewMemoryCache()
	// Required: Apply/VerifyRange silently skip real signature verification
	// without this.
	cache.SetServerPublicKey(signer.PublicKey())

	d, err := daemon.New(cache, authority)
	if err != nil {
		return nil, fmt.Errorf("construct in-process daemon: %w", err)
	}
	d.SetPersonalMode(true)

	remote, err := daemonclient.NewWithTransport(
		"in-process", controlplane.DefaultRequestTimeout, inProcessTransport{handler: d.Handler()},
	)
	if err != nil {
		return nil, fmt.Errorf("construct in-process daemon client: %w", err)
	}

	root, err := os.MkdirTemp("", "agent-comms-tui-wasm-*")
	if err != nil {
		return nil, fmt.Errorf("create demo project root: %w", err)
	}
	// Scoped to this demo's own private directory so identity's global
	// profile registry and credential store (both consulted by
	// service.Service.Register/Execute) never touch the host's real
	// machine-wide config -- the same isolation
	// internal/testsupport.StartPersonalProject and remote_test.go apply via
	// t.Setenv, done here with plain os.Setenv since this runs once at
	// process startup, not inside a *testing.T.
	if err = os.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user")); err != nil {
		return nil, fmt.Errorf("set config dir: %w", err)
	}
	if err = os.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(root, "credentials")); err != nil {
		return nil, fmt.Errorf("set credential dir: %w", err)
	}

	projectStore, err := bootstrapProject(context.Background(), d, authority, root, signer)
	if err != nil {
		return nil, fmt.Errorf("bootstrap demo project: %w", err)
	}

	return service.NewWithRemote(projectStore, remote), nil
}

// bootstrapProject creates a fresh project inside authority, issues the
// owner's real agent.register + agent.activate(OWNER) bootstrap events
// (adapted from internal/runtimeinit.go's initialCommands, see this file's
// package comment), primes d's cache with them, and writes the resulting
// store.Config to root -- everything internal/runtimeinit.Initialize's
// "personal" mode does, minus every SQLite-backed step.
func bootstrapProject(
	ctx context.Context, d *daemon.Daemon, authority *wasmdemo.MemoryAuthority, root string, signer *controlplane.Signer,
) (*store.Store, error) {
	projectID := "ac-" + uuid.NewString()

	if err := authority.CreateProject(ctx, projectID, demoOwner); err != nil {
		return nil, fmt.Errorf("create demo project: %w", err)
	}

	ownerCredential, err := identity.Generate(projectID, demoOwner)
	if err != nil {
		return nil, fmt.Errorf("generate owner credential: %w", err)
	}
	credentials := identity.DefaultStore()
	if err = credentials.Put(ownerCredential); err != nil {
		return nil, fmt.Errorf("store owner credential: %w", err)
	}

	// Matches internal/daemon/run.go's real daemon-start path (which always
	// calls this before serving any request) -- only affects /health/live's
	// JSON fields (runtime_mode, project_id), which the TUI itself never
	// consults, but this keeps the in-process daemon's wiring sequence a
	// complete reproduction of the real one rather than a partial one.
	d.SetIdentity("personal", projectID)

	for _, command := range ownerBootstrapCommands(projectID, ownerCredential) {
		if _, _, err = authority.Command(ctx, command); err != nil {
			return nil, fmt.Errorf("bootstrap owner principal: %w", err)
		}
	}
	// Prime the daemon's cache with the owner bootstrap events now, rather
	// than relying on d.command's own cache-gap recovery (it would recover
	// on the very next real command regardless, via d.Sync -- see
	// daemon.go's command handler -- but priming here is more predictable to
	// reason about and test than leaning on that recovery path).
	if err = d.Sync(ctx, projectID); err != nil {
		return nil, fmt.Errorf("prime demo project cache: %w", err)
	}

	config := store.Config{
		SchemaVersion: model.SchemaVersion, ToolkitVersion: store.RuntimeVersion, ToolkitBuildID: store.RuntimeBuildID,
		MinimumToolkit:       store.RuntimeVersion,
		ProjectFormatVersion: store.ProjectFormatVersion, ManagedFilesVersion: store.ManagedFilesVersion,
		RuntimeMode: "personal", ServicePublicKey: signer.PublicKey(), DaemonEndpoint: "in-process",
		ProjectID: projectID, Owner: demoOwner, DefaultLease: "4h", StaleGrace: "1h",
		ActiveRetention: "168h", SummaryLimit: 1200, ArtifactLimitBytes: 5 * 1024 * 1024,
	}
	config.ManagedFileHashes = store.ManagedHashes(config)

	runtimePath := filepath.Join(root, store.Runtime)
	// "cache" mirrors internal/runtimeinit.go's writeRuntimeFiles. Of that
	// function's other three directories ("artifacts/sha256", "data",
	// "tmp"), only "artifacts/sha256" is actually reachable from the live
	// TUI: service.Service.AddArtifact (internal/service/service.go, wired
	// to internal/tui/artifacts.go) writes directly to
	// filepath.Join(root, store.Runtime, "artifacts", "sha256", sum) via
	// os.WriteFile with no MkdirAll of its own -- it assumes a real
	// project's directory already exists. "data" and "tmp" have no
	// reachable caller anywhere in internal/service or internal/tui (grepped
	// for both literal path segments across both packages; the only
	// callers of DraftPath/ProjectionPath/DatabasePath are runtimeinit and
	// daemon.Run, neither of which this in-process demo daemon uses), so
	// they are deliberately left out rather than created speculatively.
	for _, directory := range []string{"cache", filepath.Join("artifacts", "sha256")} {
		if err = os.MkdirAll(filepath.Join(runtimePath, directory), 0o700); err != nil {
			return nil, err
		}
	}
	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(runtimePath, "config.json"), append(configJSON, '\n'), 0o600); err != nil {
		return nil, err
	}

	return store.Open(root), nil
}

// ownerBootstrapCommands returns the owner's real, signed agent.register +
// agent.activate(OWNER) commands -- the exact same pair
// internal/runtimeinit.go's initialCommands builds for a real personal-mode
// project, adapted here to be issued against a *wasmdemo.MemoryAuthority
// (via bootstrapProject, above) instead of personalauthority.Engine.
func ownerBootstrapCommands(projectID string, credential identity.Credential) []controlplane.Command {
	payloads := []struct {
		eventType string
		payload   any
	}{
		{"agent.register", model.AgentRegistered{PublicKey: credential.PublicKey, PrincipalType: model.PrincipalHuman, DisplayName: demoOwner}},
		{"agent.activate", model.AgentActivated{Role: model.RoleOwner, Capabilities: []string{"*"}, Scopes: []string{"*"}}},
	}
	commands := make([]controlplane.Command, 0, len(payloads))
	for _, item := range payloads {
		payload, _ := model.EncodePayload(item.eventType, item.payload)
		command := controlplane.Command{
			ProjectID: projectID, Actor: demoOwner, Type: item.eventType, EntityID: demoOwner,
			Payload: payload, IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
		}
		if item.eventType == "agent.register" {
			command.PublicKey = credential.PublicKey
		}
		_ = command.Sign(credential.PrivateKey)
		commands = append(commands, command)
	}
	return commands
}
