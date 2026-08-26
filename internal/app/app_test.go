package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
	"github.com/DhanushSantosh/AgentComms/internal/daemon"
	"github.com/DhanushSantosh/AgentComms/internal/daemonclient"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/interactiveserve"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
	"github.com/DhanushSantosh/AgentComms/internal/runtimeinit"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/sessionbind"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/creack/pty"
)

func TestArtifactVerifyPlainOutputIsAReadableReceipt(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"init", "--project", project, "--non-interactive", "--owner", "owner", "--json"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	contents := []byte("release bundle")
	digest := sha256.Sum256(contents)
	hash := fmt.Sprintf("%x", digest)
	artifactPath := filepath.Join(project, store.Runtime, "artifacts", "sha256", hash)
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if err := Run([]string{"artifact", "verify", "--project", project, "--sha256", hash, "--output", "plain"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	plain := stdout.String()
	if strings.HasPrefix(strings.TrimSpace(plain), "{") {
		t.Fatalf("plain artifact verification exposed JSON: %s", plain)
	}
	for _, want := range []string{"Artifact verified", hash, "14 bytes"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("plain artifact verification is missing %q: %s", want, plain)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("plain artifact verification wrote diagnostics: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := Run([]string{"artifact", "verify", "--project", project, "--sha256", hash, "--json"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var envelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("artifact verification JSON contract is invalid: %v\n%s", err, stdout.String())
	}
	if !envelope.OK || envelope.Command != "artifact.verify" {
		t.Fatalf("artifact verification JSON contract changed: %#v", envelope)
	}
}

func TestGenericBoundedCommandPlainOutputNeverFallsBackToJSON(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"init", "--project", project, "--non-interactive", "--owner", "owner", "--json"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(project, "release.txt")
	if err := os.WriteFile(source, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := Run([]string{"artifact", "add", "--project", project, "--path", source, "--output", "plain"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	plain := stdout.String()
	if strings.HasPrefix(strings.TrimSpace(plain), "{") || strings.Contains(plain, "\"api_version\"") {
		t.Fatalf("bounded command fell back to JSON:\n%s", plain)
	}
	for _, want := range []string{"Artifact add", "Sha256", "release.txt"} {
		if !strings.Contains(strings.ToLower(plain), strings.ToLower(want)) {
			t.Fatalf("bounded command output is missing %q:\n%s", want, plain)
		}
	}
}

func TestFoundationHealthCommandsHaveIntentionalPlainViews(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"init", "--project", project, "--non-interactive", "--owner", "owner", "--json"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		command []string
		want    []string
	}{
		{command: []string{"status"}, want: []string{"Project status", "Agents", "Tasks", "Invocations", "Integrity"}},
		{command: []string{"verify"}, want: []string{"Integrity verified", "Events", "Consistency", "Connectivity"}},
		{command: []string{"doctor"}, want: []string{"Project health", "Integrity", "Findings", "Binary"}},
	}
	for _, test := range tests {
		stdout.Reset()
		stderr.Reset()
		args := append(test.command, "--project", project, "--output", "plain")
		if err := Run(args, &stdout, &stderr); err != nil {
			t.Fatalf("%s: %v", strings.Join(test.command, " "), err)
		}
		for _, want := range test.want {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("%s output is missing %q:\n%s", strings.Join(test.command, " "), want, stdout.String())
			}
		}
		if strings.ContainsAny(strings.TrimSpace(stdout.String())[:1], "{[") {
			t.Fatalf("%s exposed a serialization shape:\n%s", strings.Join(test.command, " "), stdout.String())
		}
	}
}

func TestMain(testingMain *testing.M) {
	launchDaemonProcess = func(_, projectRoot string, _ io.Writer) error {
		projectStore := store.Open(projectRoot)
		config, err := projectStore.Config()
		if err != nil {
			return err
		}
		credential, err := identity.ResolveCredential(
			projectStore.Credentials, config.ProjectID, "__personal_authority__",
		)
		if err != nil {
			return err
		}
		go func() {
			_ = daemon.Run(context.Background(), daemon.RunConfig{
				ServicePublicKey: config.ServicePublicKey,
				CachePath:        runtimeinit.ProjectionPath(projectRoot), Endpoint: config.DaemonEndpoint,
				RuntimeMode: "personal", PersonalDatabase: runtimeinit.DatabasePath(projectRoot),
				ServicePrivateKey: credential.PrivateKey, ProjectID: config.ProjectID,
				ProductVersion: Version, BuildID: buildinfo.ResolvedBuildID(),
				ProjectFormatVersion: store.ProjectFormatVersion,
				CacheSchemaVersion:   projectlifecycle.ProjectionCacheSchemaVersion,
				DraftSchemaVersion:   projectlifecycle.DraftStoreSchemaVersion,
				ProjectRoot:          projectRoot,
				ConnectorConfigPath:  os.Getenv("AGENT_COMMS_CONNECTOR_CONFIG"),
			})
		}()
		return nil
	}
	os.Exit(testingMain.Run())
}

func cleanupProjectDaemon(t *testing.T, projectRoot string) {
	t.Helper()
	t.Cleanup(func() {
		projectStore := store.Open(projectRoot)
		config, err := projectStore.Config()
		if err != nil {
			return
		}
		client, err := daemonclient.New(config.DaemonEndpoint, 300*time.Millisecond)
		if err != nil {
			t.Errorf("prepare daemon cleanup: %v", err)
			return
		}
		healthContext, cancelHealth := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, healthErr := client.Health(healthContext)
		cancelHealth()
		if healthErr != nil {
			return
		}
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		shutdownErr := client.Shutdown(shutdownContext)
		cancelShutdown()
		if shutdownErr != nil {
			t.Errorf("shut down test daemon: %v", shutdownErr)
			return
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			probeContext, cancelProbe := context.WithTimeout(context.Background(), 100*time.Millisecond)
			_, probeErr := client.Health(probeContext)
			cancelProbe()
			if probeErr != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Error("test daemon did not stop before cleanup")
	})
}

func TestClaudeAttachDoesNotRequireInitializedProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/runtimes/runtime-one/events" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "text/event-stream")
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"claude", "attach", "--runtime", "runtime-one", "--server", server.URL}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
}

// TestCodexAttachDoesNotRequireInitializedProject guards against exactly
// the bug found live this session: codex serve/attach were added to
// PersistentPreRunE's project-init exemption list initially only for
// claude, so `codex attach` failed with "open project runtime: ... no
// such file or directory" from any directory that wasn't itself an
// initialized agent-comms project, even though attach has nothing to do
// with this project's own store.
func TestCodexAttachDoesNotRequireInitializedProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/runtimes/runtime-one/events" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "text/event-stream")
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"codex", "attach", "--runtime", "runtime-one", "--server", server.URL}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
}

func TestVersionEnvelope(t *testing.T) {
	var out, err bytes.Buffer
	if e := Run([]string{"version", "--json"}, &out, &err); e != nil {
		t.Fatal(e)
	}
	var v Envelope
	if e := json.Unmarshal(out.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	if !v.OK || v.APIVersion != APIVersion {
		t.Fatalf("bad envelope: %#v", v)
	}
}

func TestVersionPlainOutputIsHumanReadableAndJSONStaysCompatible(t *testing.T) {
	var out, err bytes.Buffer
	if e := Run([]string{"version", "--output", "plain"}, &out, &err); e != nil {
		t.Fatal(e)
	}
	plain := out.String()
	if strings.HasPrefix(strings.TrimSpace(plain), "{") {
		t.Fatalf("plain version output exposed JSON: %s", plain)
	}
	for _, want := range []string{"Agent Comms", "Version", "Schema", "Project format", "Platform"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("plain version output is missing %q: %s", want, plain)
		}
	}
	if err.Len() != 0 {
		t.Fatalf("plain version output wrote diagnostics: %s", err.String())
	}

	out.Reset()
	err.Reset()
	if e := Run([]string{"version", "--json"}, &out, &err); e != nil {
		t.Fatal(e)
	}
	var envelope Envelope
	if e := json.Unmarshal(out.Bytes(), &envelope); e != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", e, out.String())
	}
	if !envelope.OK || envelope.APIVersion != APIVersion || envelope.Command != "version" {
		t.Fatalf("--json envelope changed: %#v", envelope)
	}
}

func TestConflictingOutputModesAreRejected(t *testing.T) {
	var out, err bytes.Buffer
	e := Run([]string{"version", "--json", "--output", "plain"}, &out, &err)
	if e == nil {
		t.Fatal("expected conflicting --json and --output plain flags to fail")
	}
	if !strings.Contains(e.Error(), "conflicting output") {
		t.Fatalf("expected an actionable conflict error, got %v", e)
	}
	if out.Len() != 0 {
		t.Fatalf("conflicting output modes wrote a result: %s", out.String())
	}
}

func TestRenderTableAlignsColumnsAndHandlesEmptyRows(t *testing.T) {
	var out bytes.Buffer
	renderTable(&out, []string{"ID", "STATUS"}, [][]string{
		{"builder", "ACTIVE"},
		{"a-much-longer-id", "SUSPENDED"},
	})
	want := "ID                STATUS\n" +
		"builder           ACTIVE\n" +
		"a-much-longer-id  SUSPENDED\n"
	if out.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", out.String(), want)
	}

	out.Reset()
	renderTable(&out, []string{"ID", "STATUS"}, nil)
	if out.String() != "(no rows)\n" {
		t.Fatalf("expected the empty-rows placeholder, got %q", out.String())
	}
}

// TestAgentListHumanOutputIsATableNotIndentedJSON is the regression test
// for a real, confirmed-live-all-session friction point: every CLI
// command's non-JSON output path still printed pretty-*indented* JSON
// (json.MarshalIndent), not an actual table -- a human doing an ad hoc
// check still had to mentally parse it either way, and --json continued
// to be the only way to get genuinely structured output. agent/runtime/
// invocation list now render as an aligned plain-text table by default;
// --json is unaffected (still the same Envelope-wrapped JSON any existing
// script or --json caller already depends on).
func TestAgentListHumanOutputIsATableNotIndentedJSON(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(jsonMode bool, args ...string) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project)
		if jsonMode {
			args = append(args, "--json")
		}
		if err := Run(args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	run(true, "init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	run(true, "agent", "register", "--id", "builder")
	run(true, "agent", "activate", "--id", "builder", "--role", "AGENT", "--scope", "src", "--actor", "owner")

	run(false, "agent", "list")
	if strings.HasPrefix(strings.TrimSpace(out.String()), "{") {
		t.Fatalf("expected table output, not JSON, without --json: %s", out.String())
	}
	if !strings.Contains(out.String(), "ID") || !strings.Contains(out.String(), "builder") ||
		!strings.Contains(out.String(), "ACTIVE") {
		t.Fatalf("expected a table with builder's row, got: %s", out.String())
	}

	run(true, "agent", "list")
	var envelope Envelope
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, out.String())
	}
	if !envelope.OK || envelope.Command != "agent.list" {
		t.Fatalf("--json behavior changed unexpectedly: %#v", envelope)
	}
}

// TestAgentSwitchRoleIsSelfServiceThroughTheRealCLI is the end-to-end
// regression test for RFC 0018's self-service role switch, exercised
// through the real CLI entry point (Run), not just Service directly: a
// plain, non-elevated agent relabels its own role with no owner or
// orchestrator action at all, and the change is visible in agent list.
func TestAgentSwitchRoleIsSelfServiceThroughTheRealCLI(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		if err := Run(args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	run("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	run("agent", "register", "--id", "builder")
	run("agent", "activate", "--id", "builder", "--role", "Backend-Designer", "--scope", "src", "--actor", "owner")

	// Self-service: no --actor owner/orchestrator elevation needed, unlike
	// agent activate above -- builder switches its own role directly.
	run("agent", "switch-role", "--role", "Frontend-Architect", "--actor", "builder")

	run("agent", "list")
	if !bytes.Contains(out.Bytes(), []byte(`"role":"Frontend-Architect"`)) {
		t.Fatalf("expected builder's role to show Frontend-Architect after self-service switch: %s", out.String())
	}
}

// TestTUIResolvesOwnerWhenLegacyActorIsAmbiguous is the end-to-end
// regression test for RFC 0019: the TUI's own PersistentPreRunE wiring
// (cmd.Name() == "tui") must resolve straight to the project owner, with
// AmbiguousActor left false, in exactly the scenario RFC 0017 refuses for
// every other command -- a project with 2+ locally-registered identities
// and no recognized provider session. Invokes PersistentPreRunE directly
// rather than through Run() (RunE would enter the TUI's real, blocking
// bubbletea event loop, which needs a real terminal this test doesn't have).
func TestTUIResolvesOwnerWhenLegacyActorIsAmbiguous(t *testing.T) {
	d := t.TempDir()
	cleanupProjectDaemon(t, d)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(d, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(d, "credentials"))
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CODEX_THREAD_ID", "")

	var out, errBuf bytes.Buffer
	if err := Run([]string{"init", "--project", d, "--non-interactive", "--owner", "owner", "--json"}, &out, &errBuf); err != nil {
		t.Fatalf("init failed: %v\n%s", err, errBuf.String())
	}
	out.Reset()
	errBuf.Reset()
	if err := Run([]string{"agent", "register", "--id", "helper", "--principal-type", "AGENT",
		"--project", d, "--actor", "helper", "--json"}, &out, &errBuf); err != nil {
		t.Fatalf("registering a second local identity failed: %v\n%s", err, errBuf.String())
	}

	c := &cli{out: &out, err: &errBuf, timeout: 10 * time.Second}
	root := c.root()
	// c.root() binds --project's flag default (registered as part of
	// building the command tree) into c.project, overwriting any value
	// set before this call -- so it has to be set after, not in the cli
	// struct literal above.
	c.project = d
	root.SetOut(&out)
	root.SetErr(&errBuf)
	tuiCmd, _, findErr := root.Find([]string{"tui"})
	if findErr != nil {
		t.Fatal(findErr)
	}
	if err := root.PersistentPreRunE(tuiCmd, nil); err != nil {
		t.Fatalf("tui's own PersistentPreRunE failed: %v", err)
	}
	if c.svc.AmbiguousActor {
		t.Fatalf("expected the TUI to resolve straight to the project owner, not refuse as ambiguous: %+v", c.actorResolution)
	}
	if c.actorResolution.Actor != "owner" || c.actorResolution.Source != identity.ActorSourceProjectOwner {
		t.Fatalf("expected the TUI to resolve to the project owner, got %+v", c.actorResolution)
	}

	// The identical scenario through an ordinary (non-tui) command must
	// still refuse exactly as RFC 0017 specifies -- this opt-out is
	// scoped to the TUI alone.
	c2 := &cli{out: &out, err: &errBuf, timeout: 10 * time.Second}
	root2 := c2.root()
	c2.project = d
	root2.SetOut(&out)
	root2.SetErr(&errBuf)
	agentListCmd, _, findErr2 := root2.Find([]string{"agent", "list"})
	if findErr2 != nil {
		t.Fatal(findErr2)
	}
	if err := root2.PersistentPreRunE(agentListCmd, nil); err != nil {
		t.Fatalf("agent list's own PersistentPreRunE failed: %v", err)
	}
	if !c2.svc.AmbiguousActor {
		t.Fatalf("expected an ordinary CLI command to still be refused as ambiguous, unaffected by RFC 0019: %+v", c2.actorResolution)
	}
}

// TestEnsureDaemonReplacesIncompatibleDaemon guards ensureDaemon's
// replace-on-mismatch path end to end: a running daemon that reports a
// stale BuildID/ProductVersion must be shut down and replaced with a
// fresh one, not silently reused. It reuses the same fake-daemon harness
// TestMain already installs (launchDaemonProcess overridden to run
// daemon.Run in-process instead of spawning a real subprocess) so the
// replacement daemon it launches genuinely answers on the same endpoint.
func TestEnsureDaemonReplacesIncompatibleDaemon(t *testing.T) {
	root := t.TempDir()
	cleanupProjectDaemon(t, root)
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	if _, err := runtimeinit.Initialize(context.Background(), runtimeinit.Config{
		ProjectRoot: root, Owner: "owner", Mode: "personal",
	}); err != nil {
		t.Fatal(err)
	}
	projectStore := store.Open(root)
	config, err := projectStore.Config()
	if err != nil {
		t.Fatal(err)
	}
	credential, err := identity.ResolveCredential(projectStore.Credentials, config.ProjectID, "__personal_authority__")
	if err != nil {
		t.Fatal(err)
	}

	staleCtx, stopStale := context.WithCancel(context.Background())
	defer stopStale()
	go func() {
		_ = daemon.Run(staleCtx, daemon.RunConfig{
			ServicePublicKey: config.ServicePublicKey, CachePath: runtimeinit.ProjectionPath(root),
			Endpoint: config.DaemonEndpoint, RuntimeMode: "personal", PersonalDatabase: runtimeinit.DatabasePath(root),
			ServicePrivateKey: credential.PrivateKey, ProjectID: config.ProjectID,
			ProductVersion: "stale-version", BuildID: "stale-build",
		})
	}()

	client, err := daemonclient.New(config.DaemonEndpoint, 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	var staleHealth daemonclient.Health
	// 200 attempts, not the original 50: this fixture's stale daemon is a
	// goroutine started moments earlier, and this loop's per-attempt cost
	// is small (a failed dial returns near-instantly, it doesn't wait out
	// the 300ms context timeout), so 50 attempts was really only ~1-2s of
	// real budget -- confirmed too tight on a real Windows CI runner
	// (named-pipe dial, not a Unix socket) on a clean re-run of the exact
	// same commit, distinct from the SQLITE_BUSY race daemonShutdownWaitAttempts
	// fixes elsewhere in this file.
	for attempt := 0; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		staleHealth, err = client.Health(ctx)
		cancel()
		if err == nil {
			break
		}
		if attempt >= 200 {
			t.Fatalf("fixture stale daemon never became healthy: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if staleHealth.BuildID != "stale-build" {
		t.Fatalf("fixture daemon did not report the expected stale build ID: %+v", staleHealth)
	}

	if err = ensureDaemon(root, config); err != nil {
		t.Fatal(err)
	}
	freshHealth, err := client.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if freshHealth.BuildID == "stale-build" || freshHealth.BuildID != buildinfo.ResolvedBuildID() {
		t.Fatalf("expected ensureDaemon to replace the stale daemon with one reporting the current build ID, got: %+v", freshHealth)
	}
	if freshHealth.ProductVersion != Version {
		t.Fatalf("expected the replacement daemon to report the current product version, got: %+v", freshHealth)
	}
}

// TestHandoffProjectUpgradePropagatesChildErrorCode guards finding 7's
// handoff fix: when the spawned (already-updated) child binary's `project
// upgrade --json` run fails with a classified error -- UPGRADE_REQUIRED is
// the normal, expected "needs confirmation" outcome, not a real failure --
// the parent `update apply` must surface that exact code, not collapse it
// into a generic UPGRADE_FAILED.
func TestHandoffProjectUpgradePropagatesChildErrorCode(t *testing.T) {
	client := &cli{
		json: true, out: &bytes.Buffer{}, err: &bytes.Buffer{},
		handoffRunner: func(
			_ context.Context,
			_ string,
			_ []string,
			_ io.Reader,
			_ io.Writer,
			stderr io.Writer,
		) error {
			_, writeErr := io.WriteString(stderr, `{"api_version":"agent-comms/v1","ok":false,"command":"project upgrade","error":{"code":"UPGRADE_REQUIRED","message":"project upgrade requires explicit confirmation"}}`)
			if writeErr != nil {
				return writeErr
			}
			return errors.New("child exited unsuccessfully")
		},
	}
	_, err := client.handoffProjectUpgrade(context.Background(), "fake-agent-comms", t.TempDir(), false, false)
	if err == nil {
		t.Fatal("expected handoffProjectUpgrade to return an error when the child reports one")
	}
	lifecycleErr, ok := err.(*projectlifecycle.Error)
	if !ok {
		t.Fatalf("expected a *projectlifecycle.Error, got %T: %v", err, err)
	}
	if lifecycleErr.Code != projectlifecycle.CodeUpgradeRequired {
		t.Fatalf("code=%q, want %q -- the child's real classified error must survive the handoff, not collapse into a generic failure", lifecycleErr.Code, projectlifecycle.CodeUpgradeRequired)
	}
}

func TestProfileListDoesNotRequireInitializedProject(t *testing.T) {
	configDirectory := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", configDirectory)
	// This test exercises the legacy, machine-wide ActiveProfile field
	// deliberately (a plain human terminal with no recognized provider
	// session) -- clear both session vars explicitly rather than assume a
	// "clean" environment, since this suite itself typically runs inside a
	// real Claude Code session that sets CLAUDE_CODE_SESSION_ID. See RFC
	// 0016: with a real session ID present, this would correctly resolve
	// to that session's own (unset) active profile instead, not the
	// legacy field this test means to check.
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CODEX_THREAD_ID", "")
	profile := identity.Profile{
		Name:        "project:owner",
		ProjectID:   "project",
		Actor:       "owner",
		ProjectRoot: filepath.Join(t.TempDir(), "missing-project"),
	}
	if err := identity.SaveUserConfig(identity.UserConfig{
		ActiveProfile: profile.Name,
		Profiles:      map[string]identity.Profile{profile.Name: profile},
	}); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := Run([]string{"profile", "list", "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("profile list should not require an initialized working directory: %v\n%s", err, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"active":"project:owner"`)) {
		t.Fatalf("profile list did not return the registry: %s", stdout.String())
	}
}

// TestWriteRefusesAmbiguousLegacyActorAcrossMultipleProfiles is the
// end-to-end regression test for RFC 0017, exercised through the real CLI
// entry point (Run), not just Service directly -- confirms the guard is
// actually wired into internal/app's PersistentPreRunE, not merely present
// in the Service type. Reproduces the shape of a real, confirmed-live
// incident: a project with more than one locally-registered identity,
// where a bare invocation with no recognized provider session and no
// explicit actor used to silently sign under whichever identity happened
// to be sitting in the shared legacy slot.
func TestWriteRefusesAmbiguousLegacyActorAcrossMultipleProfiles(t *testing.T) {
	d := t.TempDir()
	cleanupProjectDaemon(t, d)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(d, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(d, "credentials"))
	// Deliberately no recognized provider session -- the exact condition
	// that makes the legacy fallback reachable at all.
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CODEX_THREAD_ID", "")

	var out, errBuf bytes.Buffer
	if err := Run([]string{"init", "--project", d, "--non-interactive", "--owner", "owner", "--json"}, &out, &errBuf); err != nil {
		t.Fatalf("init failed: %v\n%s", err, errBuf.String())
	}

	// Registering a second local identity for this same project is what
	// makes the legacy fallback genuinely ambiguous -- one profile alone
	// (just "owner") would still be safe.
	out.Reset()
	errBuf.Reset()
	if err := Run([]string{"agent", "register", "--id", "helper", "--principal-type", "AGENT", "--project", d, "--actor", "helper", "--json"}, &out, &errBuf); err != nil {
		t.Fatalf("registering a second local identity failed: %v\n%s", err, errBuf.String())
	}

	// A bare write, no --actor, no session: must now be refused instead of
	// silently signing under whichever identity the shared legacy field
	// happens to hold.
	out.Reset()
	errBuf.Reset()
	err := Run([]string{"task", "create", "--id", "task-1", "--title", "t", "--repository", "r", "--branch", "b", "--resource", "src/x", "--project", d, "--json"}, &out, &errBuf)
	if err == nil {
		t.Fatalf("expected the ambiguous bare write to be refused, got success: %s", out.String())
	}
	if !bytes.Contains(errBuf.Bytes(), []byte("more than one locally-registered identity")) {
		t.Fatalf("refusal did not explain itself as ambiguous-actor related; stdout=%s stderr=%s", out.String(), errBuf.String())
	}

	// The same write, made explicit, must succeed -- the guard blocks
	// ambiguity, not the write itself.
	out.Reset()
	errBuf.Reset()
	if err := Run([]string{"task", "create", "--id", "task-1", "--title", "t", "--repository", "r", "--branch", "b", "--resource", "src/x", "--project", d, "--actor", "owner", "--json"}, &out, &errBuf); err != nil {
		t.Fatalf("expected the same write to succeed once the actor is explicit: %v\n%s", err, errBuf.String())
	}
}

func TestInitInNonGitDir(t *testing.T) {
	d := t.TempDir()
	var out, err bytes.Buffer
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(d, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(d, "credentials"))
	e := Run([]string{"init", "--project", d, "--non-interactive", "--owner", "owner", "--json"}, &out, &err)
	if e != nil {
		t.Fatalf("non-Git init should succeed: %v", e)
	}
	config, configErr := service.New(d).Store.Config()
	if configErr != nil {
		t.Fatal(configErr)
	}
	if config.RuntimeMode != "personal" {
		t.Fatalf("new project did not default to personal mode: %+v", config)
	}
}

func TestInitRejectsInvalidModeBeforeWritingRuntime(t *testing.T) {
	project := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	err := Run([]string{"init", "--project", project, "--non-interactive", "--owner", "owner", "--mode", "unknown", "--json"}, &out, &stderr)
	if err == nil {
		t.Fatal("invalid mode was accepted")
	}
	if !bytes.Contains(stderr.Bytes(), []byte(`"code":"VALIDATION"`)) {
		t.Fatalf("invalid mode did not use the stable validation code: %s", stderr.String())
	}
	if _, statErr := os.Stat(filepath.Join(project, ".agent-comms")); !os.IsNotExist(statErr) {
		t.Fatalf("invalid mode wrote a runtime: %v", statErr)
	}
}

// TestUnknownFlagStillEmitsJSONError guards a real, confirmed-live bug
// found during a hands-on Windows compatibility pass: an unknown flag
// (e.g. a typo) fails inside cobra's own flag parsing, before
// PersistentPreRunE ever runs -- so c.json, which that function binds,
// was never set to true even though --json was right there in argv.
// Run's error branch used to check only c.json, so the JSON envelope was
// silently never written; main.go separately assumes Run already handled
// printing whenever --json is literally present in os.Args, so neither
// layer printed anything at all, on any platform (confirmed live on both
// Git Bash and a genuine native PowerShell console, ruling out a
// redirection artifact). Falling back to a raw scan of args
// (ContainsJSONFlag) closes the gap.
func TestUnknownFlagStillEmitsJSONError(t *testing.T) {
	project := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	err := Run([]string{"decision", "create", "--project", project, "--id", "d1", "--this-flag-does-not-exist", "x", "--json"}, &out, &stderr)
	if err == nil {
		t.Fatal("expected an unknown flag to produce an error")
	}
	if stderr.Len() == 0 {
		t.Fatal("expected an unknown flag with --json to still emit a JSON-formatted error, got no output at all")
	}
	if !bytes.Contains(stderr.Bytes(), []byte(`"ok":false`)) || !bytes.Contains(stderr.Bytes(), []byte(`"error"`)) {
		t.Fatalf("expected a valid JSON error envelope, got: %s", stderr.String())
	}
}

func TestCompletion(t *testing.T) {
	var out, err bytes.Buffer
	if e := Run([]string{"completion", "powershell"}, &out, &err); e != nil {
		t.Fatal(e)
	}
	if out.Len() < 100 {
		t.Fatal("completion output too small")
	}
}

// TestHostLabelResolvesActorAcrossInvocations proves the per-project,
// per-host actor resolution round trip: a connection that sets
// AGENT_COMMS_HOST_LABEL and never passes --actor self-registers once as a
// project-chosen ID, then automatically resolves to that same actor on a
// later, independent command with no further prompting.
func TestHostLabelResolvesActorAcrossInvocations(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	cmd := exec.Command("git", "init")
	cmd.Dir = project
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(string(b))
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	t.Setenv("AGENT_COMMS_HOST_LABEL", "claude-test")
	var out, stderr bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		if err := Run(args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	run("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	run("agent", "register", "--id", "AXIOM")
	run("profile", "current")
	if !bytes.Contains(out.Bytes(), []byte(`"actor":"AXIOM"`)) ||
		!bytes.Contains(out.Bytes(), []byte(`"source":"host_binding"`)) {
		t.Fatalf("profile.current did not explain the host-bound actor: %s", out.String())
	}
	run("agent", "activate", "--id", "AXIOM", "--actor", "owner", "--role", "AGENT", "--scope", "src")
	run("message", "post", "--id", "msg1", "--kind", "FYI", "--to", "owner", "--subject", "test", "--body", "hello")
	if !bytes.Contains(out.Bytes(), []byte(`"actor":"AXIOM"`)) {
		t.Fatalf("message.post did not resolve to the host-labeled profile's actor: %s", out.String())
	}
	run("history")
	if !bytes.Contains(out.Bytes(), []byte(`"actor":"AXIOM"`)) || !bytes.Contains(out.Bytes(), []byte(`"type":"message.post"`)) {
		t.Fatalf("history did not show message.post authored by the resolved actor: %s", out.String())
	}
}

// TestAgentRegisterCLIEnforcesSponsorshipRule guards a real gap: the CLI's
// `agent register` previously had zero authorization check at all when
// registering a different id than the acting actor — anyone with the
// binary could register (or squat) any unrelated identity. Registering a
// different id now requires the acting actor to be an active orchestrator
// or human principal, matching the same rule the MCP agent_register tool
// enforces (internal/mcp/server.go), via the shared
// Service.CanSponsorRegistration.
// grantOrchestratorCLI drives the two-step apply-then-approve flow the
// ORCHESTRATOR role now requires (internal/protocol/transitions.go) through
// the CLI: approver applies a HUMAN-tier approval for this exact grant,
// approves it, then activates id as ORCHESTRATOR.
func grantOrchestratorCLI(t *testing.T, must func(args ...string), approver, id string) {
	t.Helper()
	approvalID := id + "-orchestrator-approval"
	must("approval", "request", "--actor", approver, "--id", approvalID, "--tier", "HUMAN", "--action", "agent.activate:"+id)
	must("approval", "approve", "--actor", approver, "--id", approvalID)
	must("agent", "activate", "--actor", approver, "--id", id, "--role", "ORCHESTRATOR", "--scope", "src")
}

// TestAgentElevateKeyCLIRefusesWithoutInteractiveTerminal guards the actual
// security property the elevated key depends on: promptNewPassphrase
// (internal/app/passphrase.go) must refuse outright, not hang or silently
// read whatever bytes happen to be on stdin, when it isn't attached to a
// real terminal -- exactly what `go test`'s own stdin looks like, and
// exactly what an automated caller (a script, an MCP-connected agent, or an
// agent shelling out to this CLI) always looks like.
func TestAgentElevateKeyCLIRefusesWithoutInteractiveTerminal(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		return Run(args, &out, &stderr)
	}
	if e := run("init", "--non-interactive", "--owner", "owner", "--mode", "personal"); e != nil {
		t.Fatalf("init: %v\n%s", e, stderr.String())
	}
	e := run("agent", "elevate-key", "--actor", "owner")
	if e == nil {
		t.Fatal("expected agent elevate-key to refuse without an interactive terminal")
	}
	if code := errorCode(e); code != "VALIDATION" {
		t.Fatalf("expected VALIDATION, got %s: %v", code, e)
	}
	if !strings.Contains(e.Error(), "interactive terminal") {
		t.Fatalf("expected an interactive-terminal-shaped error, got: %v", e)
	}
}

func TestInitInteractiveOffersElevatedKeySetupAndDegradesGracefullyWithoutTTY(t *testing.T) {
	project := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	// Deliberately NOT --non-interactive and NOT --json: init's own
	// interactive prompts (owner, y/N confirm, elevated-key offer) share the
	// same writer as --json's output envelope, so a real interactive run
	// never combines the two. stdin has no TTY attached in a test process --
	// the exact same "declined or couldn't answer" case the design's doctor
	// half exists to catch.
	e := Run([]string{"init", "--project", project, "--owner", "owner", "--mode", "personal", "--yes"}, &out, &stderr)
	if e != nil {
		t.Fatalf("init should still succeed even though elevated-key setup can't complete without a TTY: %v\n%s", e, stderr.String())
	}
	if !strings.Contains(out.String(), "elevated-key setup failed") || !strings.Contains(out.String(), "interactive terminal") {
		t.Fatalf("expected a TTY-shaped elevated-key skip message, got: %s", out.String())
	}
}

func TestDoctorWarnsWhenOwnerHasNoElevatedKeyAndClearsOnceRegistered(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	if e := Run([]string{"init", "--project", project, "--non-interactive", "--owner", "owner", "--mode", "personal", "--json"}, &out, &stderr); e != nil {
		t.Fatalf("init: %v\n%s", e, stderr.String())
	}
	out.Reset()
	stderr.Reset()
	// doctor deliberately never starts the daemon itself (PersistentPreRunE
	// skips ensureDaemon for it); a warm-up command that does start it is
	// needed first, same as TestDoctorReportsRuntimeAndBootstrapProblems.
	if e := Run([]string{"agent", "register", "--project", project, "--id", "builder", "--json"}, &out, &stderr); e != nil {
		t.Fatalf("agent register: %v\n%s", e, stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if e := Run([]string{"doctor", "--project", project, "--json"}, &out, &stderr); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(out.Bytes(), []byte("NO_ELEVATED_KEY")) {
		t.Fatalf("expected doctor to warn NO_ELEVATED_KEY for an owner with no elevated key: %s", out.String())
	}
	svc := service.New(project)
	if _, e := svc.ElevateKey("owner", "correct horse battery staple"); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	stderr.Reset()
	if e := Run([]string{"doctor", "--project", project, "--json"}, &out, &stderr); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(out.Bytes(), []byte("NO_ELEVATED_KEY")) {
		t.Fatalf("expected doctor NOT to warn once the owner has an elevated key: %s", out.String())
	}
}

func TestAgentDeleteCLIRequiresReasonAndAllowsIDReuse(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	must("agent", "register", "--actor", "owner", "--id", "candidate")
	var originalRegistration struct {
		Result struct {
			KeyFingerprint string `json:"key_fingerprint"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &originalRegistration); err != nil {
		t.Fatal(err)
	}
	must("agent", "activate", "--actor", "owner", "--id", "candidate", "--role", "AGENT", "--scope", "src")
	must("agent", "revoke", "--actor", "owner", "--id", "candidate", "--reason", "retired")

	if err := run("agent", "delete", "--actor", "owner", "--id", "candidate"); err == nil {
		t.Fatal("expected agent delete without --reason to be rejected")
	}
	must("agent", "delete", "--actor", "owner", "--id", "candidate", "--reason", "remove retired identity")
	if !bytes.Contains(out.Bytes(), []byte(`"type":"agent.delete"`)) ||
		!bytes.Contains(out.Bytes(), []byte(`"key_fingerprint":"`)) {
		t.Fatalf("agent.delete output did not include the event and signing-key fingerprint: %s", out.String())
	}
	must("agent", "list", "--actor", "owner")
	if bytes.Contains(out.Bytes(), []byte(`"candidate"`)) {
		t.Fatalf("deleted candidate remained in agent list: %s", out.String())
	}
	must("agent", "register", "--actor", "owner", "--id", "candidate", "--display-name", "Replacement")
	var replacementRegistration struct {
		Result struct {
			KeyFingerprint string `json:"key_fingerprint"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &replacementRegistration); err != nil {
		t.Fatal(err)
	}
	if originalRegistration.Result.KeyFingerprint == replacementRegistration.Result.KeyFingerprint {
		t.Fatal("re-registered identity reused the original signing key")
	}
	must("history", "--actor", "owner")
	if !bytes.Contains(out.Bytes(), []byte(`"actor_key_fingerprint":"`)) ||
		!bytes.Contains(out.Bytes(), []byte(`"type":"agent.delete"`)) {
		t.Fatalf("filtered history did not expose the deletion fingerprint boundary: %s", out.String())
	}
	must("history", "--key-fingerprint", replacementRegistration.Result.KeyFingerprint)
	if bytes.Contains(out.Bytes(), []byte(originalRegistration.Result.KeyFingerprint)) ||
		!bytes.Contains(out.Bytes(), []byte(replacementRegistration.Result.KeyFingerprint)) {
		t.Fatalf("history key-fingerprint filter did not isolate the replacement identity: %s", out.String())
	}
	must("search", "agent.register", "--key-fingerprint", replacementRegistration.Result.KeyFingerprint)
	if bytes.Contains(out.Bytes(), []byte(originalRegistration.Result.KeyFingerprint)) ||
		!bytes.Contains(out.Bytes(), []byte(replacementRegistration.Result.KeyFingerprint)) {
		t.Fatalf("search key-fingerprint filter did not isolate the replacement identity: %s", out.String())
	}
}

func TestAgentDeleteCLIRefusesElevatedSigningWithoutInteractiveTerminal(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	must("agent", "register", "--actor", "owner", "--id", "candidate")
	must("agent", "activate", "--actor", "owner", "--id", "candidate", "--role", "AGENT", "--scope", "src")
	must("agent", "revoke", "--actor", "owner", "--id", "candidate", "--reason", "retired")

	instance := service.New(project)
	if _, err := instance.ElevateKey("owner", "correct passphrase"); err != nil {
		t.Fatalf("register elevated key: %v", err)
	}
	err := run("agent", "delete", "--actor", "owner", "--id", "candidate", "--reason", "remove retired identity")
	if err == nil {
		t.Fatal("expected elevated deletion to refuse without an interactive terminal")
	}
	if !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("expected an interactive-terminal-shaped error, got: %v", err)
	}
}

func TestAgentRegisterCLIEnforcesSponsorshipRule(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if e := run(args...); e != nil {
			t.Fatalf("%v: %v\n%s", args, e, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal")

	// The project owner (an active HUMAN principal by construction) may
	// register on behalf of a different id.
	must("agent", "register", "--actor", "owner", "--id", "lead")
	grantOrchestratorCLI(t, must, "owner", "lead")

	// An active ORCHESTRATOR-role agent principal may also sponsor a
	// registration on behalf of a different id.
	must("agent", "register", "--actor", "lead", "--id", "sponsored-agent")

	must("agent", "register", "--actor", "owner", "--id", "reviewer")
	must("agent", "activate", "--actor", "owner", "--id", "reviewer", "--role", "AGENT", "--scope", "src")

	// A plain, active AGENT-role principal must be rejected when
	// registering a different id.
	if e := run("agent", "register", "--actor", "reviewer", "--id", "someone-else"); e == nil {
		t.Fatal("expected a plain agent's sponsorship attempt to be rejected")
	} else if code := errorCode(e); code != "AUTHORIZATION" {
		t.Fatalf("expected AUTHORIZATION, got %s: %v", code, e)
	}

	must("status")
	if bytes.Contains(out.Bytes(), []byte(`"someone-else"`)) {
		t.Fatal("rejected sponsorship attempt must not have registered the principal")
	}

	// Self-registration never requires sponsorship, regardless of role.
	must("agent", "register", "--actor", "fresh-self", "--id", "fresh-self")
}

// TestAgentActivateCLIRequiresHumanToGrantOrchestratorRole guards the same
// orchestrator-escalation hard check (internal/protocol/transitions.go) at
// the CLI entry point: an AGENT-principal orchestrator must not be able to
// grant the orchestrator role to anyone else, even though it already
// passes the ordinary owner-or-orchestrator elevation gate that lets it
// call `agent activate` at all.
func TestAgentActivateCLIRequiresHumanToGrantOrchestratorRole(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if e := run(args...); e != nil {
			t.Fatalf("%v: %v\n%s", args, e, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	must("agent", "register", "--actor", "agent-lead", "--id", "agent-lead")
	grantOrchestratorCLI(t, must, "owner", "agent-lead")
	must("agent", "register", "--actor", "candidate", "--id", "candidate")

	if e := run("agent", "activate", "--actor", "agent-lead", "--id", "candidate", "--role", "ORCHESTRATOR", "--scope", "src"); e == nil {
		t.Fatal("expected an agent-principal orchestrator's grant to be rejected")
	} else if code := errorCode(e); code != "AUTHORIZATION" {
		t.Fatalf("expected AUTHORIZATION, got %s: %v", code, e)
	}

	must("status")
	var envelope struct {
		Result struct {
			Agents map[string]struct {
				Role string `json:"role"`
			} `json:"agents"`
		} `json:"result"`
	}
	if e := json.Unmarshal(out.Bytes(), &envelope); e != nil {
		t.Fatal(e)
	}
	if envelope.Result.Agents["candidate"].Role == "ORCHESTRATOR" {
		t.Fatal("candidate must not have been granted the orchestrator role")
	}

	grantOrchestratorCLI(t, must, "owner", "candidate")
}

// TestAgentRevokeCLIRejectsAgentOrchestratorRevokingAnotherOrchestrator is
// the revoke-side sibling of TestAgentActivateCLIRequiresHumanToGrantOrchestratorRole.
func TestAgentRevokeCLIRejectsAgentOrchestratorRevokingAnotherOrchestrator(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if e := run(args...); e != nil {
			t.Fatalf("%v: %v\n%s", args, e, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	must("agent", "register", "--actor", "agent-lead", "--id", "agent-lead")
	grantOrchestratorCLI(t, must, "owner", "agent-lead")
	must("agent", "register", "--actor", "other-orchestrator", "--id", "other-orchestrator")
	grantOrchestratorCLI(t, must, "owner", "other-orchestrator")

	if e := run("agent", "revoke", "--actor", "agent-lead", "--id", "other-orchestrator"); e == nil {
		t.Fatal("expected an agent-principal orchestrator's revoke to be rejected")
	} else if code := errorCode(e); code != "AUTHORIZATION" {
		t.Fatalf("expected AUTHORIZATION, got %s: %v", code, e)
	}

	must("agent", "revoke", "--actor", "owner", "--id", "other-orchestrator", "--reason", "human-approved removal")
	must("status")
	var envelope struct {
		Result struct {
			Agents map[string]struct {
				Status string `json:"status"`
			} `json:"agents"`
		} `json:"result"`
	}
	if e := json.Unmarshal(out.Bytes(), &envelope); e != nil {
		t.Fatal(e)
	}
	if envelope.Result.Agents["other-orchestrator"].Status != "REVOKED" {
		t.Fatalf("other-orchestrator was not revoked: %+v", envelope.Result.Agents["other-orchestrator"])
	}

	if e := run("agent", "revoke", "--actor", "owner", "--id", "owner"); e == nil {
		t.Fatal("expected revoking the owner to be rejected")
	}

	if e := run("agent", "activate", "--actor", "owner", "--id", "other-orchestrator", "--role", "AGENT", "--scope", "src"); e == nil {
		t.Fatal("expected reactivating a revoked principal to be rejected")
	}
}

func TestDoctorReportsRuntimeAndBootstrapProblems(t *testing.T) {
	d := t.TempDir()
	cleanupProjectDaemon(t, d)
	cmd := exec.Command("git", "init")
	cmd.Dir = d
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(string(b))
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(d, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(d, "credentials"))
	var out, err bytes.Buffer
	if e := Run([]string{"init", "--project", d, "--non-interactive", "--owner", "owner", "--mode", "personal", "--json"}, &out, &err); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	err.Reset()
	if e := Run([]string{"agent", "register", "--project", d, "--id", "builder", "--json"}, &out, &err); e != nil {
		t.Fatal(e)
	}
	svc := service.New(d)
	if _, e := svc.Execute("owner", "task.create", "stale-work", model.TaskCreated{Title: "Stale work", Repository: "local", Branch: "main", Resources: []string{"path:src/**"}}); e != nil {
		t.Fatal(e)
	}
	if _, e := svc.Execute("owner", "task.claim", "stale-work", model.TaskClaimed{LeaseUntil: time.Now().UTC().Add(-time.Hour)}); e != nil {
		t.Fatal(e)
	}
	cfgPath := filepath.Join(d, ".agent-comms", "config.json")
	var cfg map[string]any
	b, _ := os.ReadFile(cfgPath)
	_ = json.Unmarshal(b, &cfg)
	cfg["toolkit_version"] = "9.9.9"
	b, _ = json.Marshal(cfg)
	_ = os.WriteFile(cfgPath, b, 0600)
	_ = os.Remove(filepath.Join(d, ".agents"))
	_ = os.Remove(filepath.Join(d, ".agent-comms", "AGENT_INSTRUCTIONS.md"))
	out.Reset()
	err.Reset()
	if e := Run([]string{"doctor", "--project", d, "--json"}, &out, &err); e != nil {
		t.Fatal(e)
	}
	text := out.String()
	for _, code := range []string{"BINARY_RUNTIME_VERSION_MISMATCH", "MANAGED_BOOTSTRAP_MISSING", "AGENT_INSTRUCTIONS_MISSING", "TEST_LIKE_RUNTIME"} {
		if !bytes.Contains(out.Bytes(), []byte(code)) {
			t.Fatalf("doctor missing %s: %s", code, text)
		}
	}
}

func TestInvocationAndRuntimeCLIWorkflow(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		if err := Run(args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	run("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	run("agent", "register", "--id", "builder")
	run("agent", "activate", "--id", "builder", "--role", "AGENT", "--scope", "src", "--actor", "owner")
	run("runtime", "register", "--actor", "builder", "--id", "runtime-builder",
		"--agent", "builder", "--connector", "MCP", "--max-concurrent", "1")
	run("runtime", "heartbeat", "--actor", "builder", "--id", "runtime-builder")
	run("invocation", "policy", "set", "--agent", "builder", "--mode", "AUTOMATIC", "--actor", "owner")
	run("invocation", "request", "--id", "inv-cli", "--to", "builder",
		"--instruction", "Review the CLI workflow", "--priority", "URGENT", "--actor", "owner")
	run("invocation", "next", "--actor", "builder", "--runtime", "runtime-builder")
	if !bytes.Contains(out.Bytes(), []byte(`"found":true`)) ||
		!bytes.Contains(out.Bytes(), []byte(`"id":"inv-cli"`)) {
		t.Fatalf("CLI did not return the pending invocation: %s", out.String())
	}
	run("invocation", "claim", "--actor", "builder", "--id", "inv-cli", "--runtime", "runtime-builder")
	run("invocation", "start", "--actor", "builder", "--id", "inv-cli", "--summary", "started")
	run("invocation", "complete", "--actor", "builder", "--id", "inv-cli", "--summary", "done")
	run("invocation", "list", "--status", "COMPLETED")
	if !bytes.Contains(out.Bytes(), []byte(`"status":"COMPLETED"`)) {
		t.Fatalf("CLI did not return the completed invocation: %s", out.String())
	}
	run("control", "overview")
	if !bytes.Contains(out.Bytes(), []byte(`"online_runtimes"`)) ||
		!bytes.Contains(out.Bytes(), []byte(`"invocations_completed":1`)) {
		t.Fatalf("control overview did not summarize project state: %s", out.String())
	}
	run("control", "settings")
	if !bytes.Contains(out.Bytes(), []byte(`"max_delivery_attempts":10`)) {
		t.Fatalf("control settings omitted invocation limits: %s", out.String())
	}
}

// TestTaskLockCreatesAndClaimsInOneStep guards `task lock`, added to close a
// real gap: `task claim --worktree` only acquires a lock for a task that
// already exists, and `task create` requires a title, summary, repository,
// branch, and resource list first -- real ceremony for a real task, but too
// much for the single most common shape of work in an interactive
// multi-agent setup (a human directly asking a live agent to fix something,
// with no Task tracked yet). `task lock` creates a minimal task and claims
// it in one command.
func TestTaskLockCreatesAndClaimsInOneStep(t *testing.T) {
	project := t.TempDir()
	worktree := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		if err := Run(args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	run("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	run("agent", "register", "--id", "builder")
	run("agent", "activate", "--id", "builder", "--role", "AGENT", "--scope", "src", "--actor", "owner")

	run("task", "lock", "--actor", "builder", "--worktree", worktree, "--note", "fixing a bug")
	if !bytes.Contains(out.Bytes(), []byte(`"type":"task.claim"`)) {
		t.Fatalf("expected task lock to emit a task.claim event, got: %s", out.String())
	}
	// Decode rather than a raw byte-substring match against worktree: on
	// Windows, worktree contains backslashes (C:\Users\...), and JSON
	// escapes those (\ -> \\) in the emitted output, so the raw path never
	// byte-matches the encoded string -- confirmed live, this exact
	// assertion failed on windows-latest CI while passing everywhere else.
	var lockEvent struct {
		Result struct {
			Data struct {
				Worktree string `json:"worktree"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &lockEvent); err != nil {
		t.Fatalf("decode task lock output: %v\n%s", err, out.String())
	}
	if lockEvent.Result.Data.Worktree != worktree {
		t.Fatalf("expected the claim to record worktree %q, got %q (full output: %s)", worktree, lockEvent.Result.Data.Worktree, out.String())
	}

	run("task", "list")
	if !bytes.Contains(out.Bytes(), []byte(`"status":"CLAIMED"`)) ||
		!bytes.Contains(out.Bytes(), []byte(`"title":"fixing a bug"`)) {
		t.Fatalf("expected a claimed, --note-titled task to show up in task list, got: %s", out.String())
	}
}

// TestTaskLockConflictsOnlyForTheSameWorktree is the regression test for a
// bug found while building task lock: the first implementation used the
// raw worktree path as the task's Resources entry, which made task.claim's
// scope-permission check (transitions.go's scopeAllows) fail outright for
// any actor without a wildcard scope -- a filesystem path essentially never
// matches a scope tag like "src". Reusing the actor's bare scope tag
// instead fixed that, but introduced a worse, opposite bug: every ad hoc
// lock from same-scoped actors then overlapped every other one via the
// generic write-lease check (overlap()), even across completely unrelated
// worktrees -- confirmed live, locking a second, entirely different
// directory was rejected purely because an earlier lock happened to share
// the "src" scope, nowhere near the same files. The shipped fix scopes each
// lock's resource under a per-lock, id-unique sub-resource
// (scope+"/adhoc/"+id): still satisfies scopeAllows's prefix rule, but two
// separate ad hoc locks' resources can never accidentally overlap each
// other, leaving the worktree-specific check as the only thing that still
// can -- which is the one conflict this command is actually meant to catch.
func TestTaskLockConflictsOnlyForTheSameWorktree(t *testing.T) {
	project := t.TempDir()
	sharedWorktree := t.TempDir()
	otherWorktree := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	for _, id := range []string{"agent-a", "agent-b", "agent-c"} {
		must("agent", "register", "--id", id)
		must("agent", "activate", "--id", id, "--role", "AGENT", "--scope", "src", "--actor", "owner")
	}

	must("task", "lock", "--actor", "agent-a", "--worktree", sharedWorktree, "--note", "agent-a working")

	if err := run("task", "lock", "--actor", "agent-b", "--worktree", sharedWorktree, "--note", "agent-b same dir"); err == nil {
		t.Fatalf("expected agent-b locking the same worktree agent-a already holds to fail, got success: %s", out.String())
	} else if !strings.Contains(err.Error(), "already leased") {
		t.Fatalf("expected an 'already leased' worktree-conflict error, got: %v", err)
	}

	if err := run("task", "lock", "--actor", "agent-c", "--worktree", otherWorktree, "--note", "agent-c different dir"); err != nil {
		t.Fatalf("expected agent-c locking a different worktree to succeed despite sharing agent-a/b's scope, got: %v\n%s", err, stderr.String())
	}
}

// TestInvocationRequestDeliversDirectlyToRegisteredInteractiveSession guards
// the direct-invocation path decided live this session: requesting an
// invocation for a runtime with a registered interactive session (see
// internal/interactiveserve) must wake that session's real terminal as part
// of the same command, with no separate worker or polling process — the
// agent-to-agent "invoke directly" behavior the user asked for in place of a
// watcher script.
func TestInvocationRequestDeliversDirectlyToLiveInteractiveSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interactive PTY delivery is not supported on Windows")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	interactiveTempDirectory := t.TempDir()
	t.Setenv("TMPDIR", "/tmp")
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		if err := Run(args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}

	run("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	run("agent", "register", "--id", "opencode-agent")
	run("agent", "activate", "--id", "opencode-agent", "--role", "AGENT", "--scope", "src", "--actor", "owner")
	run("runtime", "register", "--actor", "opencode-agent", "--id", "opencode-runtime",
		"--agent", "opencode-agent", "--kind", "INTERACTIVE",
		"--connector", "INTERACTIVE", "--max-concurrent", "1")
	run("invocation", "policy", "set", "--agent", "opencode-agent", "--mode", "AUTOMATIC", "--actor", "owner")

	// Stand up a live session the same way `runtime interactive-serve`
	// does, without going through the CLI's Run() — that command calls
	// os.Exit on completion, which would kill this test process.
	controlMaster, controlSlave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer controlMaster.Close()
	defer controlSlave.Close()
	var stdout bytes.Buffer
	var stdoutMu sync.Mutex
	stdinR, stdinW := io.Pipe()
	t.Cleanup(func() { _ = stdinW.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	// Desktop providers commonly inherit a private TMPDIR while the daemon
	// inherits /tmp. The control path must remain identical across that
	// process boundary.
	t.Setenv("TMPDIR", interactiveTempDirectory)
	go func() {
		_, _ = interactiveserve.Serve(ctx, interactiveserve.ServeOptions{
			ProjectRoot: project, RuntimeID: "opencode-runtime",
			Command:   []string{"bash", "-c", "cat"},
			ControlFD: int(controlSlave.Fd()), Stdin: stdinR,
			Stdout: syncWriterFor(&stdoutMu, &stdout),
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !interactiveserve.Alive(context.Background(), project, "opencode-runtime") {
		if time.Now().After(deadline) {
			t.Fatal("expected the live session to become dialable")
		}
		time.Sleep(50 * time.Millisecond)
	}
	run("runtime", "heartbeat", "--actor", "opencode-agent", "--id", "opencode-runtime",
		"--endpoint-id", "test-opencode-endpoint")

	run("invocation", "request", "--id", "inv-direct", "--to", "opencode-agent",
		"--instruction", "say hi", "--consumer", "INTERACTIVE_ONLY", "--runtime", "opencode-runtime", "--actor", "owner")
	if bytes.Contains(out.Bytes(), []byte(`"warnings"`)) {
		t.Fatalf("expected no delivery warnings against a live session: %s", out.String())
	}

	deadline = time.Now().Add(5 * time.Second)
	for {
		stdoutMu.Lock()
		text := stdout.String()
		stdoutMu.Unlock()
		if strings.Contains(text, "inv-direct") && strings.Contains(text, "opencode-agent") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected the invocation to be delivered directly into the live session, got:\n%s", text)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// syncWriterFor adapts a mutex-guarded bytes.Buffer to io.Writer for a
// concurrently-written test double.
func syncWriterFor(mu *sync.Mutex, buf *bytes.Buffer) io.Writer {
	return &lockedWriter{mu: mu, buf: buf}
}

type lockedWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

// TestInvocationRequestWithoutRegisteredSessionIsUnaffected guards the
// common case — a headless worker, not a live interactive session — where
// direct delivery must stay silent and invocation.request's result shape
// must stay exactly what it always was.
func TestInvocationRequestWithoutRegisteredSessionIsUnaffected(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		if err := Run(args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	run("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	run("agent", "register", "--id", "builder")
	run("agent", "activate", "--id", "builder", "--role", "AGENT", "--scope", "src", "--actor", "owner")
	run("invocation", "policy", "set", "--agent", "builder", "--mode", "AUTOMATIC", "--actor", "owner")
	run("invocation", "request", "--id", "inv-headless", "--to", "builder", "--instruction", "say hi", "--actor", "owner")
	if bytes.Contains(out.Bytes(), []byte(`"warnings"`)) {
		t.Fatalf("expected no warnings when the target has no registered interactive session: %s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"entity_id":"inv-headless"`)) {
		t.Fatalf("expected the usual signed event shape: %s", out.String())
	}
}

// Note: the old double-binding guard tested here (registering the same
// tmux pane to two different runtimes) no longer applies — there is no
// registration step anymore. The equivalent guarantee (a runtime can't be
// double-served) is now structural: Serve's socket bind refuses outright
// when another live process already owns a runtime's deterministic socket,
// covered by TestServeSecondInstanceRefusesWhileFirstIsLive in
// internal/interactiveserve.

func TestInvocationRedeliverRejectsUnknownID(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		return Run(args, &out, &stderr)
	}
	if err := run("init", "--non-interactive", "--owner", "owner", "--mode", "personal"); err != nil {
		t.Fatalf("init: %v\n%s", err, stderr.String())
	}
	if err := run("invocation", "redeliver", "--id", "no-such-invocation"); err == nil {
		t.Fatal("expected redeliver of an unknown invocation ID to fail")
	}
}

func TestInvocationRedeliverRejectsNonPendingInvocation(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		if err := Run(args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	run("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	run("agent", "register", "--id", "builder")
	run("agent", "activate", "--id", "builder", "--role", "AGENT", "--scope", "src", "--actor", "owner")
	run("runtime", "register", "--actor", "builder", "--id", "runtime-builder",
		"--agent", "builder", "--connector", "MCP", "--max-concurrent", "1")
	run("runtime", "heartbeat", "--actor", "builder", "--id", "runtime-builder")
	run("invocation", "policy", "set", "--agent", "builder", "--mode", "AUTOMATIC", "--actor", "owner")
	run("invocation", "request", "--id", "inv-done", "--to", "builder", "--instruction", "say hi", "--actor", "owner")
	run("invocation", "claim", "--actor", "builder", "--id", "inv-done", "--runtime", "runtime-builder")
	run("invocation", "start", "--actor", "builder", "--id", "inv-done", "--summary", "started")
	run("invocation", "complete", "--actor", "builder", "--id", "inv-done", "--summary", "done")

	out.Reset()
	stderr.Reset()
	err := Run([]string{"invocation", "redeliver", "--id", "inv-done", "--runtime", "runtime-builder",
		"--actor", "owner", "--project", project, "--json"}, &out, &stderr)
	if err == nil {
		t.Fatal("expected redeliver of a COMPLETED invocation to fail")
	}
}

// TestInvocationRedeliverReachesSessionMissedByRequest guards the actual
// point of the redeliver command: a PENDING invocation whose original
// direct-delivery nudge never landed (no live session existed yet at
// request time) can be manually re-nudged once a live session comes up,
// without creating a new invocation or event.
func TestInvocationRedeliverReachesSessionMissedByRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interactive PTY delivery is not supported on Windows")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		if err := Run(args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}

	run("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	run("agent", "register", "--id", "opencode-runner")
	run("agent", "activate", "--id", "opencode-runner", "--role", "AGENT", "--scope", "src", "--actor", "owner")
	run("runtime", "register", "--actor", "opencode-runner", "--id", "opencode-runtime",
		"--agent", "opencode-runner", "--kind", "INTERACTIVE",
		"--connector", "INTERACTIVE", "--max-concurrent", "1")
	run("invocation", "policy", "set", "--agent", "opencode-runner", "--mode", "AUTOMATIC", "--actor", "owner")

	// No live session exists yet, so this request's own nudge is silently a
	// no-op — the whole point of the test is to confirm redeliver can still
	// reach the runtime later.
	run("invocation", "request", "--id", "inv-missed", "--to", "opencode-runner",
		"--instruction", "say hi", "--consumer", "INTERACTIVE_ONLY", "--runtime", "opencode-runtime", "--actor", "owner")
	if !bytes.Contains(out.Bytes(), []byte(`"outcome":"UNAVAILABLE"`)) {
		t.Fatalf("expected an unavailable delivery result while the session is offline: %s", out.String())
	}

	controlMaster, controlSlave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer controlMaster.Close()
	defer controlSlave.Close()
	var stdout bytes.Buffer
	var stdoutMu sync.Mutex
	stdinR, stdinW := io.Pipe()
	t.Cleanup(func() { _ = stdinW.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_, _ = interactiveserve.Serve(ctx, interactiveserve.ServeOptions{
			ProjectRoot: project, RuntimeID: "opencode-runtime",
			Command:   []string{"bash", "-c", "cat"},
			ControlFD: int(controlSlave.Fd()), Stdin: stdinR,
			Stdout: syncWriterFor(&stdoutMu, &stdout),
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !interactiveserve.Alive(context.Background(), project, "opencode-runtime") {
		if time.Now().After(deadline) {
			t.Fatal("expected the live session to become dialable")
		}
		time.Sleep(50 * time.Millisecond)
	}
	run("runtime", "heartbeat", "--actor", "opencode-runner", "--id", "opencode-runtime",
		"--endpoint-id", "test-redelivery-endpoint")

	run("invocation", "redeliver", "--id", "inv-missed", "--runtime", "opencode-runtime", "--actor", "owner")
	if bytes.Contains(out.Bytes(), []byte(`"warnings"`)) {
		t.Fatalf("expected no delivery warnings against a now-live session: %s", out.String())
	}

	deadline = time.Now().Add(5 * time.Second)
	for {
		stdoutMu.Lock()
		text := stdout.String()
		stdoutMu.Unlock()
		if strings.Contains(text, "inv-missed") && strings.Contains(text, "opencode-runner") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected redeliver to reach the now-live session, got:\n%s", text)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestPinInteractiveServeArgsAppliesAnExistingBinding(t *testing.T) {
	root := t.TempDir()
	if err := sessionbind.Save(root, "HENRY", "pinned-session-id", "claude"); err != nil {
		t.Fatal(err)
	}
	got := pinInteractiveServeArgs(root, "HENRY", []string{"claude", "--dangerously-skip-permissions", "--continue"})
	want := []string{"claude", "--dangerously-skip-permissions", "--resume", "pinned-session-id"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestPinInteractiveServeArgsNoOpWithoutAnyBinding(t *testing.T) {
	root := t.TempDir()
	in := []string{"claude", "--continue"}
	got := pinInteractiveServeArgs(root, "HENRY", in)
	if len(got) != len(in) {
		t.Fatalf("expected args untouched with no binding on record, got %v", got)
	}
	for i := range in {
		if got[i] != in[i] {
			t.Fatalf("expected args untouched with no binding on record, got %v", got)
		}
	}
}

func TestPinInteractiveServeArgsOnlyAppliesTheMatchingRuntimesBinding(t *testing.T) {
	root := t.TempDir()
	if err := sessionbind.Save(root, "HULK", "hulks-session-id", "agy"); err != nil {
		t.Fatal(err)
	}
	in := []string{"claude", "--continue"}
	got := pinInteractiveServeArgs(root, "HENRY", in)
	if len(got) != len(in) {
		t.Fatalf("expected HENRY's args untouched by HULK's binding, got %v", got)
	}
	for i := range in {
		if got[i] != in[i] {
			t.Fatalf("expected HENRY's args untouched by HULK's binding, got %v", got)
		}
	}
}

func TestWithClaudeAllowAgentCommsRejectsNonClaudeCommand(t *testing.T) {
	executablePath := func() (string, error) { return "/usr/bin/agent-comms", nil }
	if _, err := withClaudeAllowAgentComms([]string{"bash"}, executablePath); err == nil {
		t.Fatal("expected an error when the wrapped command is not claude")
	}
	if _, err := withClaudeAllowAgentComms(nil, executablePath); err == nil {
		t.Fatal("expected an error for an empty command")
	}
}

func TestWithClaudeAllowAgentCommsAppendsScopedAllowedTools(t *testing.T) {
	dir := t.TempDir()
	fakeExe := filepath.Join(dir, "agent-comms")
	if err := os.WriteFile(fakeExe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	executablePath := func() (string, error) { return fakeExe, nil }

	got, err := withClaudeAllowAgentComms([]string{"claude", "--resume", "abc"}, executablePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolvedFakeExecutable, err := filepath.EvalSymlinks(fakeExe)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "--resume", "abc",
		"--allowedTools", "Bash(" + resolvedFakeExecutable + " *)",
		"--allowedTools", "Bash(agent-comms *)"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestStripLaunchTerminalFlag(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"bare form", []string{"runtime", "interactive-serve", "--id", "x", "--launch-terminal", "--", "claude"},
			[]string{"runtime", "interactive-serve", "--id", "x", "--", "claude"}},
		{"equals form", []string{"runtime", "interactive-serve", "--launch-terminal=true", "--id", "x"},
			[]string{"runtime", "interactive-serve", "--id", "x"}},
		{"absent", []string{"runtime", "interactive-serve", "--id", "x"},
			[]string{"runtime", "interactive-serve", "--id", "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripLaunchTerminalFlag(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			}
		})
	}
}

// TestInteractiveServeRejectsClaudeAllowAgentCommsForOtherCommands guards the
// CLI wiring itself (not just the extracted helper): the flag's validation
// must run and return an error before interactive-serve ever tries to open a
// pty or call os.Exit, so this is safe to run through Run() directly.
func TestInteractiveServeRejectsClaudeAllowAgentCommsForOtherCommands(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	if err := Run([]string{"init", "--non-interactive", "--owner", "owner", "--mode", "personal",
		"--project", project, "--json"}, &out, &stderr); err != nil {
		t.Fatalf("init: %v\n%s", err, stderr.String())
	}
	out.Reset()
	stderr.Reset()
	err := Run([]string{"runtime", "interactive-serve", "--id", "not-claude", "--claude-allow-agent-comms",
		"--project", project, "--json", "--", "bash", "-c", "true"}, &out, &stderr)
	if err == nil {
		t.Fatal("expected --claude-allow-agent-comms to reject a non-claude wrapped command")
	}
}
