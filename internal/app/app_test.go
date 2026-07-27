package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/daemon"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/interactiveserve"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/runtimeinit"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/creack/pty"
)

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
			})
		}()
		return nil
	}
	os.Exit(testingMain.Run())
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
func TestAgentRegisterCLIEnforcesSponsorshipRule(t *testing.T) {
	project := t.TempDir()
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
	must("agent", "activate", "--actor", "owner", "--id", "lead", "--role", "ORCHESTRATOR", "--scope", "src")

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
	must("agent", "activate", "--actor", "owner", "--id", "agent-lead", "--role", "ORCHESTRATOR", "--scope", "src")
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

	must("agent", "activate", "--actor", "owner", "--id", "candidate", "--role", "ORCHESTRATOR", "--scope", "src")
}

func TestDoctorReportsRuntimeAndBootstrapProblems(t *testing.T) {
	d := t.TempDir()
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
	run("agent", "activate", "--id", "builder", "--role", "AGENT", "--scope", "src")
	run("runtime", "register", "--actor", "builder", "--id", "runtime-builder",
		"--agent", "builder", "--connector", "MCP", "--max-concurrent", "1")
	run("invocation", "policy", "set", "--agent", "builder", "--mode", "AUTOMATIC")
	run("invocation", "request", "--id", "inv-cli", "--to", "builder",
		"--instruction", "Review the CLI workflow", "--priority", "URGENT")
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

// TestInvocationRequestDeliversDirectlyToRegisteredInteractiveSession guards
// the direct-invocation path decided live this session: requesting an
// invocation for a runtime with a registered interactive session (see
// internal/interactiveserve) must wake that session's real terminal as part
// of the same command, with no separate worker or polling process — the
// agent-to-agent "invoke directly" behavior the user asked for in place of a
// watcher script.
func TestInvocationRequestDeliversDirectlyToLiveInteractiveSession(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	project := t.TempDir()
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
	run("agent", "activate", "--id", "opencode-agent", "--role", "AGENT", "--scope", "src")
	run("runtime", "register", "--actor", "opencode-agent", "--id", "opencode-runtime",
		"--agent", "opencode-agent", "--connector", "MANUAL", "--max-concurrent", "1")
	run("invocation", "policy", "set", "--agent", "opencode-agent", "--mode", "AUTOMATIC")

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

	run("invocation", "request", "--id", "inv-direct", "--to", "opencode-agent", "--instruction", "say hi")
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

func TestInteractiveRuntimeCandidatesResolveAgentOwnership(t *testing.T) {
	state := model.State{AgentRuntimes: map[string]model.AgentRuntime{
		"builder-z":       {ID: "builder-z", AgentID: "builder", Status: "ONLINE"},
		"builder-a":       {ID: "builder-a", AgentID: "builder", Status: "ONLINE"},
		"builder-drained": {ID: "builder-drained", AgentID: "builder", Status: "DRAINING"},
		"other-runtime":   {ID: "other-runtime", AgentID: "other", Status: "ONLINE"},
	}}
	got := interactiveRuntimeCandidates(state, "builder")
	want := []string{"builder", "builder-a", "builder-z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected runtime candidates: got %v want %v", got, want)
	}
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
	run("agent", "activate", "--id", "builder", "--role", "AGENT", "--scope", "src")
	run("invocation", "policy", "set", "--agent", "builder", "--mode", "AUTOMATIC")
	run("invocation", "request", "--id", "inv-headless", "--to", "builder", "--instruction", "say hi")
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
	run("agent", "activate", "--id", "builder", "--role", "AGENT", "--scope", "src")
	run("runtime", "register", "--actor", "builder", "--id", "runtime-builder",
		"--agent", "builder", "--connector", "MCP", "--max-concurrent", "1")
	run("invocation", "policy", "set", "--agent", "builder", "--mode", "AUTOMATIC")
	run("invocation", "request", "--id", "inv-done", "--to", "builder", "--instruction", "say hi")
	run("invocation", "claim", "--actor", "builder", "--id", "inv-done", "--runtime", "runtime-builder")
	run("invocation", "start", "--actor", "builder", "--id", "inv-done", "--summary", "started")
	run("invocation", "complete", "--actor", "builder", "--id", "inv-done", "--summary", "done")

	out.Reset()
	stderr.Reset()
	err := Run([]string{"invocation", "redeliver", "--id", "inv-done", "--project", project, "--json"}, &out, &stderr)
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
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	project := t.TempDir()
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
	run("agent", "activate", "--id", "opencode-runner", "--role", "AGENT", "--scope", "src")
	run("invocation", "policy", "set", "--agent", "opencode-runner", "--mode", "AUTOMATIC")

	// No live session exists yet, so this request's own nudge is silently a
	// no-op — the whole point of the test is to confirm redeliver can still
	// reach the runtime later.
	run("invocation", "request", "--id", "inv-missed", "--to", "opencode-runner", "--instruction", "say hi")
	if bytes.Contains(out.Bytes(), []byte(`"warnings"`)) {
		t.Fatalf("expected no warnings when no live session exists yet: %s", out.String())
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
			ProjectRoot: project, RuntimeID: "opencode-runner",
			Command:   []string{"bash", "-c", "cat"},
			ControlFD: int(controlSlave.Fd()), Stdin: stdinR,
			Stdout: syncWriterFor(&stdoutMu, &stdout),
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !interactiveserve.Alive(context.Background(), project, "opencode-runner") {
		if time.Now().After(deadline) {
			t.Fatal("expected the live session to become dialable")
		}
		time.Sleep(50 * time.Millisecond)
	}

	run("invocation", "redeliver", "--id", "inv-missed")
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
	want := []string{"claude", "--resume", "abc",
		"--allowedTools", "Bash(" + fakeExe + " *)",
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

// TestInteractiveServeRejectsClaudeAllowAgentCommsForOtherCommands guards the
// CLI wiring itself (not just the extracted helper): the flag's validation
// must run and return an error before interactive-serve ever tries to open a
// pty or call os.Exit, so this is safe to run through Run() directly.
func TestInteractiveServeRejectsClaudeAllowAgentCommsForOtherCommands(t *testing.T) {
	project := t.TempDir()
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
