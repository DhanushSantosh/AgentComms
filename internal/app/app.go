package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/failure"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/sessionbind"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	runtimeworker "github.com/DhanushSantosh/AgentComms/internal/worker"
	"github.com/spf13/cobra"
)

var Version = buildinfo.Version

const APIVersion = "agent-comms/v1"

const (
	interactiveHeartbeatInterval = 15 * time.Second
	// daemonReadyTimeout bounds how long ensureDaemon waits for a newly
	// spawned daemon to answer a health check before giving up. Widened
	// from the original 10s: under `-race` (which adds real per-goroutine
	// scheduling overhead) combined with genuine CI/system contention,
	// TestEnsureDaemonReplacesIncompatibleDaemon's in-process daemon
	// goroutine (its TestMain replaces the real subprocess spawn with one,
	// see app_test.go) intermittently didn't get scheduled in time to bind
	// and answer even one health check within 10s, on a clean re-run of
	// the exact same commit with no code changes. Widened again from 20s
	// to 40s: even at 20s this test kept hitting the ceiling specifically
	// on GitHub's macOS-latest runners (confirmed on three separate,
	// unrelated PRs in one session, always resolved by a bare rerun of the
	// identical commit) -- macOS Actions runners are known to be
	// meaningfully slower/more contended than the Linux/Windows ones for
	// CPU-bound work like this. 40s gives real headroom there too without
	// meaningfully changing production behavior -- a real subprocess
	// normally becomes healthy in milliseconds, so this ceiling is rarely
	// reached at all outside exactly this kind of CI contention.
	daemonReadyTimeout         = 40 * time.Second
	daemonReadyPollInterval    = 100 * time.Millisecond
	daemonHealthRequestTimeout = 300 * time.Millisecond
	// daemonShutdownWaitAttempts/daemonShutdownWaitSleep bound how long
	// ensureDaemon waits for an incompatible daemon to actually stop
	// responding before spawning its replacement. This must comfortably
	// exceed the graceful-shutdown allowance daemon.Run itself grants
	// (internal/daemon/run.go's daemonShutdownTimeout, 10s) -- otherwise a
	// daemon that legitimately uses its full shutdown budget (e.g.
	// draining an in-flight request) hasn't released its socket yet when
	// the replacement tries to bind. That bind then fails silently
	// (ListenLocal sees a still-live socket and returns os.ErrExist), and
	// ensureDaemon burns its entire daemonReadyTimeout waiting for a
	// daemon that never actually started. The previous ~3s budget
	// (20 attempts * 150ms) intermittently lost this race under real
	// scheduling jitter. 100 attempts * (health-check timeout + sleep) =
	// 15s comfortably exceeds the 10s the old daemon is allowed to take.
	daemonShutdownWaitAttempts = 100
	daemonShutdownWaitSleep    = 50 * time.Millisecond
)

type Envelope struct {
	APIVersion string     `json:"api_version"`
	OK         bool       `json:"ok"`
	Command    string     `json:"command"`
	Result     any        `json:"result,omitempty"`
	Delivery   any        `json:"delivery,omitempty"`
	Error      *ErrorBody `json:"error,omitempty"`
	Warnings   []string   `json:"warnings,omitempty"`
}

// StreamEnvelope is the stable one-record-per-line contract for commands
// whose natural result is a stream.
type StreamEnvelope struct {
	APIVersion string     `json:"api_version"`
	Command    string     `json:"command"`
	Event      string     `json:"event"`
	Timestamp  time.Time  `json:"timestamp"`
	Data       any        `json:"data,omitempty"`
	Error      *ErrorBody `json:"error,omitempty"`
}
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}
type ExitError struct {
	Code     int
	Kind     string
	Err      error
	Reported bool
}

func (e *ExitError) Error() string { return e.Err.Error() }

// ContainsJSONFlag reports whether --json is literally present in args.
// Used both here (to still emit a JSON-formatted error when a cobra-level
// parse failure happens before c.json's normal flag binding runs) and by
// cmd/agent-comms/main.go (to avoid double-printing a plain-text error
// alongside one Run already emitted as JSON) -- kept as one canonical
// implementation shared by both, rather than two copies that could drift
// out of sync with each other about what counts as "the --json flag was
// requested."
func ContainsJSONFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

type cli struct {
	out, err                                    io.Writer
	json, jsonl, nonInteractive, noColor, quiet bool
	verbose, details                            bool
	output                                      string
	project, profile, actor                     string
	timeout                                     time.Duration
	svc                                         *service.Service
	cmd                                         string
	actorResolution                             identity.ActorResolution
	pendingWarnings                             []string
	processExitCode                             int
	handoffRunner                               commandRunner
}

type commandRunner func(
	context.Context,
	string,
	[]string,
	io.Reader,
	io.Writer,
	io.Writer,
) error

func runCommand(
	ctx context.Context,
	executable string,
	arguments []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) error {
	process := exec.CommandContext(ctx, executable, arguments...)
	process.Stdin = stdin
	process.Stdout = stdout
	process.Stderr = stderr
	return process.Run()
}

var launchDaemonProcess = func(executable, projectRoot string, output io.Writer) error {
	process := exec.Command(executable, "daemon", "serve", "--project", projectRoot)
	process.Stdout = output
	process.Stderr = output
	if err := process.Start(); err != nil {
		return err
	}
	return process.Process.Release()
}

func Run(args []string, stdout, stderr io.Writer) error {
	buildinfo.Version = Version
	store.RuntimeVersion = Version
	store.RuntimeBuildID = buildinfo.ResolvedBuildID()
	c := &cli{out: stdout, err: stderr, timeout: 10 * time.Second}
	root := c.root()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SilenceUsage = true
	root.SilenceErrors = true
	e := root.Execute()
	if e != nil {
		// c.json is bound by cobra's normal flag parsing, which a
		// pre-execution parse failure (an unknown flag, in particular)
		// short-circuits before that binding ever happens -- confirmed
		// live: `decision create --summary <invalid-flag> --json` left
		// c.json false despite --json being right there in argv, and
		// printed nothing at all on any platform tested, because
		// main.go's own containsJSONFlag(os.Args[1:]) check separately
		// assumes this branch already handled it whenever --json is
		// literally present. Falling back to a raw scan of args here
		// closes that gap without either layer needing to trust the
		// other's assumption about who's responsible.
		reported := false
		if containsOutputJSONL(args) {
			_ = json.NewEncoder(stderr).Encode(StreamEnvelope{
				APIVersion: APIVersion, Command: c.cmd, Event: "error", Timestamp: time.Now().UTC(),
				Error: &ErrorBody{Code: errorCode(e), Message: e.Error()},
			})
			reported = true
		} else if c.json || ContainsJSONFlag(args) || containsOutputJSON(args) {
			body := Envelope{APIVersion: APIVersion, OK: false, Command: c.cmd, Error: &ErrorBody{Code: errorCode(e), Message: e.Error()}}
			_ = json.NewEncoder(stderr).Encode(body)
			reported = true
		} else {
			mode := cliui.Mode(c.output)
			if mode != cliui.ModePlain {
				mode = cliui.ModeHuman
			}
			_ = (cliui.Presenter{
				Out: stderr, Mode: mode,
				Capabilities: cliui.DetectCapabilities(stderr, c.noColor),
			}).RenderError(errorCode(e), e.Error(), errorHint(errorCode(e)))
			reported = true
		}
		return &ExitError{Code: exitCode(e), Kind: errorCode(e), Err: e, Reported: reported}
	}
	if c.processExitCode != 0 {
		err := fmt.Errorf("wrapped process exited with status %d", c.processExitCode)
		return &ExitError{Code: c.processExitCode, Kind: "PROCESS_EXIT", Err: err}
	}
	return nil
}

func containsOutputJSON(args []string) bool {
	for index, argument := range args {
		if argument == "--output=json" {
			return true
		}
		if argument == "--output" && index+1 < len(args) && args[index+1] == "json" {
			return true
		}
	}
	return false
}

func containsOutputJSONL(args []string) bool {
	for index, argument := range args {
		if argument == "--output=jsonl" {
			return true
		}
		if argument == "--output" && index+1 < len(args) && args[index+1] == "jsonl" {
			return true
		}
	}
	return false
}

func errorHint(code string) string {
	switch code {
	case "VALIDATION":
		return "Run the command with --help to review required flags and accepted values."
	case "AUTHORIZATION":
		return "Check the active actor, profile, role, and granted scopes."
	case "CONFLICT", "STALE_PRECONDITION":
		return "Refresh the current project state, then retry against the latest sequence."
	case "OFFLINE", "UNAVAILABLE":
		return "Check runtime connectivity and retry."
	default:
		return "Run the command with --help or use --verbose for more operational context."
	}
}

// projectScope classifies how much project context a command's
// PersistentPreRunE must establish before RunE. See RFC 0027 section 12.
type projectScope int

const (
	projectRequired projectScope = iota // an initialized project is mandatory; a missing one is an error
	projectOptional                     // use the project when the cwd has one; otherwise run degraded with c.svc == nil
	projectUserOnly                     // never touch a project; user-installation reconcile only
	projectExempt                       // touch nothing (servers, init, version, completion, project upgrade)
)

func classifyProjectScope(cmd *cobra.Command) projectScope {
	name := cmd.Name()
	path := cmd.CommandPath()
	switch {
	case name == "version", name == "init", name == "completion",
		name == "update" && cmd.Parent() == cmd.Root(),
		strings.HasPrefix(path, "agent-comms project upgrade"),
		path == "agent-comms daemon serve",
		path == "agent-comms live serve", path == "agent-comms live attach",
		path == "agent-comms runtime verify-adapter":
		return projectExempt
	case path == "agent-comms update check", path == "agent-comms update apply",
		path == "agent-comms profile list", path == "agent-comms profile use",
		path == "agent-comms config theme":
		return projectUserOnly
	case name == "config", name == "doctor", name == "agent-instructions",
		path == "agent-comms profile current":
		return projectOptional
	default:
		return projectRequired
	}
}

func (c *cli) root() *cobra.Command {
	r := &cobra.Command{
		Use:   "agent-comms",
		Short: "Governed coordination for concurrent agents",
		Long: "Agent Comms coordinates agents through signed project state, explicit ownership, and auditable transitions.\n\n" +
			"Output contracts: --output human|plain|json|jsonl. Human is TTY-aware; plain is stable text; JSON preserves the versioned envelope; JSONL is available only for natural streams.",
		Example: "  agent-comms status\n" +
			"  agent-comms task list --output plain\n" +
			"  agent-comms watch --output jsonl\n" +
			"  agent-comms invocation request --to builder --instruction \"Run tests\" --json",
		Version: Version, PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			c.cmd = cmd.CommandPath()
			if c.json && cmd.Flags().Changed("output") && c.output != string(cliui.ModeJSON) {
				return errors.New("conflicting output modes: --json requires --output json")
			}
			switch c.output {
			case "", string(cliui.ModeHuman), string(cliui.ModePlain):
			case string(cliui.ModeJSON):
				c.json = true
			case string(cliui.ModeJSONL):
				if cmd.CommandPath() != "agent-comms watch" && cmd.CommandPath() != "agent-comms invocation listen" {
					return errors.New("--output jsonl is not supported by this command")
				}
				c.jsonl = true
			default:
				return fmt.Errorf("invalid output mode %q (expected human, plain, json, or jsonl)", c.output)
			}
			scope := classifyProjectScope(cmd)
			if scope == projectExempt {
				return nil
			}
			if scope == projectUserOnly {
				warnings, e := c.reconcileUserInstallation(cmd.Context(), "")
				c.pendingWarnings = append(c.pendingWarnings, warnings...)
				return e
			}
			root := c.project
			if root == "" {
				var e error
				root, e = os.Getwd()
				if e != nil {
					return e
				}
			}
			// projectOptional commands run project-less when the current
			// directory has no initialized project. They set c.svc to nil
			// and each RunE checks for it -- see RFC 0027 section 12.
			if scope == projectOptional {
				if _, ok := currentInitializedProject(root); !ok {
					warnings, e := c.reconcileUserInstallation(cmd.Context(), "")
					c.pendingWarnings = append(c.pendingWarnings, warnings...)
					c.svc = nil
					return e
				}
			}
			applyLifecycle := scope == projectRequired && cmd.Name() != "doctor"
			if applyLifecycle {
				warnings, e := c.reconcileUserInstallation(cmd.Context(), root)
				c.pendingWarnings = append(c.pendingWarnings, warnings...)
				if e != nil {
					return e
				}
			}
			if _, e := projectlifecycle.Reconcile(cmd.Context(), projectlifecycle.Options{
				Root: root, Version: Version, BuildID: buildinfo.ResolvedBuildID(),
				Apply: applyLifecycle, Timeout: c.timeout, StopDaemon: applyLifecycle,
			}); e != nil {
				return e
			}
			if scope == projectOptional || cmd.Name() == "doctor" {
				c.svc = service.NewTolerant(root)
			} else {
				c.svc = service.New(root)
			}
			if _, err := runtimeworker.LoadProjectAdapters(root); err != nil {
				c.pendingWarnings = append(c.pendingWarnings, fmt.Sprintf("project adapters: %v", err))
			}
			switch cmd.Name() {
			case "mcp":
				c.svc.PassphrasePrompt = nonInteractivePassphrasePrompt("an MCP connection")
			case "tui":
				c.svc.PassphrasePrompt = nonInteractivePassphrasePrompt("the TUI")
			default:
				c.svc.PassphrasePrompt = promptPassphrase
			}
			cfg, e := c.svc.Store.Config()
			if e != nil {
				return fmt.Errorf("open project runtime: %w", e)
			}
			if e = c.svc.Store.EnsureRuntimeHidden(); e != nil {
				return e
			}
			c.svc.Store.LockTimeout = c.timeout
			c.svc.SetRemoteRecovery(func() error { return ensureDaemon(root, cfg) })
			if scope == projectRequired && cmd.Name() != "doctor" && (cfg.RuntimeMode == "service" || cfg.RuntimeMode == "personal") {
				if e = ensureDaemon(root, cfg); e != nil {
					return e
				}
			}
			environmentActor := os.Getenv("AGENT_COMMS_ACTOR")
			userConfig := identity.UserConfig{Profiles: map[string]identity.Profile{}}
			needsUserConfig := c.actor == "" && (c.profile != "" || environmentActor == "")
			if needsUserConfig {
				userConfig, e = identity.LoadUserConfig()
				if e != nil {
					return fmt.Errorf("load identity profiles: %w", e)
				}
			}
			providerSessionID, _ := sessionbind.Capture()
			c.actorResolution, e = identity.ResolveActor(identity.ActorResolutionRequest{
				ProjectID: cfg.ProjectID, ProjectOwner: cfg.Owner,
				ExplicitActor: c.actor, ExplicitProfile: c.profile,
				EnvironmentActor: environmentActor, HostLabel: os.Getenv("AGENT_COMMS_HOST_LABEL"),
				ProviderSessionID: providerSessionID,
				UserConfig:        userConfig,
				// Only the TUI: a real, attached, interactive terminal session
				// nothing session-less could plausibly be driving -- see RFC
				// 0019. CLI/MCP/worker are unaffected, matching cmd.Name() !=
				// "doctor" above as the established per-command check pattern.
				PreferOwnerOnAmbiguousLegacy: cmd.Name() == "tui",
			})
			if e != nil {
				return e
			}
			c.actor = c.actorResolution.Actor
			// Refuse to sign anything under an actor this ambiguously resolved
			// -- see RFC 0017. Only ActorSourceActiveProfile (the legacy,
			// machine-wide fallback used when no recognized provider session
			// is present at all) is ever ambiguous; every other resolution
			// source is either explicit or already provably unambiguous.
			c.svc.AmbiguousActor = c.actorResolution.Source == identity.ActorSourceActiveProfile &&
				userConfig.ProfileCountForProject(cfg.ProjectID) > 1
			return nil
		}}
	r.AddGroup(
		&cobra.Group{ID: "start", Title: "Getting started"},
		&cobra.Group{ID: "coordinate", Title: "Coordination"},
		&cobra.Group{ID: "identity", Title: "Identity and runtimes"},
		&cobra.Group{ID: "knowledge", Title: "Knowledge and state"},
		&cobra.Group{ID: "operations", Title: "Operations and integrations"},
	)
	f := r.PersistentFlags()
	f.StringVar(&c.project, "project", "", "target project root")
	f.StringVar(&c.profile, "profile", "", "user profile name")
	f.StringVar(&c.actor, "actor", "", "actor override (credential must match)")
	f.BoolVar(&c.json, "json", false, "emit a versioned JSON envelope")
	f.StringVar(&c.output, "output", "human", "output format: human, plain, json, or jsonl")
	f.BoolVar(&c.nonInteractive, "non-interactive", false, "never prompt")
	f.DurationVar(&c.timeout, "timeout", 10*time.Second, "transaction lock timeout")
	f.BoolVar(&c.noColor, "no-color", false, "disable ANSI color")
	f.BoolVarP(&c.quiet, "quiet", "q", false, "suppress non-essential output")
	f.BoolVarP(&c.verbose, "verbose", "v", false, "show operational metadata in human output")
	f.BoolVar(&c.details, "details", false, "show secondary and nested fields in human output")
	r.AddCommand(c.versionCmd(), c.initCmd(), c.projectCmd(), c.doctorCmd(), c.verifyCmd(), c.statusCmd(), c.attentionCmd(), c.historyCmd(), c.agentCmd(), c.runtimeCmd(), c.invocationCmd(), c.taskCmd(), c.messageCmd(), c.approvalCmd(), c.artifactCmd(), c.documentCmd(), c.envCmd(), c.draftCmd(), c.archiveCmd(), c.exportCmd(), c.profileCmd(), c.configCmd(), c.updateCmd(), c.completionCmd(r), c.agentInstructionsCmd(), c.mcpCmd(), c.watchCmd(), c.tuiCmd(), c.daemonCmd(), c.liveCmd())
	configureRootHelp(r)
	return r
}

func configureRootHelp(root *cobra.Command) {
	groups := map[string]string{
		"version": "start", "init": "start", "status": "start", "doctor": "start", "verify": "start", "tui": "start",
		"task": "coordinate", "message": "coordinate", "approval": "coordinate", "invocation": "coordinate", "attention": "coordinate",
		"agent": "identity", "runtime": "identity", "profile": "identity",
		"artifact": "knowledge", "document": "knowledge", "env": "knowledge", "draft": "knowledge", "archive": "knowledge", "history": "knowledge",
		"project": "operations", "config": "operations", "update": "operations", "watch": "operations", "export": "operations", "agent-instructions": "operations", "completion": "operations", "mcp": "operations", "live": "operations",
	}
	descriptions := map[string]string{
		"agent": "Manage governed identities, roles, scopes, and keys", "approval": "Request and resolve governed approvals",
		"archive": "Archive eligible completed project state", "artifact": "Store, inspect, and verify content-addressed artifacts",
		"attention":  "List everything currently needing operator intervention",
		"completion": "Generate shell completion source", "config": "Inspect and set user and project configuration",
		"doctor":   "Diagnose project health and actionable findings",
		"document": "Create and manage governed project documents", "env": "Manage governed project environment values",
		"export": "Export project history as JSONL or Markdown", "history": "Inspect and search the signed event timeline",
		"live":    "Serve, attach to, or tail a provider's live agent sessions",
		"message": "Post messages and manage recipient obligations", "mcp": "Serve the protocol-only MCP stdio interface",
		"profile": "Inspect and select local signing profiles",
		"status":  "Show a concise project operational summary",
		"task":    "Coordinate ownership, leases, handoffs, and task lifecycle", "tui": "Open the full-screen control room",
		"update": "Check for and install verified Agent Comms releases", "verify": "Verify the signed project event chain",
		"version": "Show binary, schema, and project format versions", "watch": "Stream changes that require operator attention",
	}
	for _, command := range root.Commands() {
		command.GroupID = groups[command.Name()]
		if command.Short == "" {
			command.Short = descriptions[command.Name()]
		}
	}
}

type invocationDeliveryResult = service.InvocationDeliveryResult

func (c *cli) invocationDeliveryOutcome(invocationID, deliveryID string) (invocationDeliveryResult, error) {
	// Retry a few times with short delays to handle the eventual-consistency
	// window between the event commit and the daemon processing it.
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		state, err := c.svc.State()
		if err != nil {
			return invocationDeliveryResult{}, err
		}
		localHostID, _ := identity.LoadHostID()
		result, exists := service.SummarizeInvocationDelivery(state, invocationID, deliveryID, localHostID)
		if exists {
			return result, nil
		}
		lastErr = errors.New("invocation not found after commit")
		if attempt < 2 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return invocationDeliveryResult{}, lastErr
}

// captureRuntimeSession opportunistically binds a freshly registered runtime
// to the current Claude conversation, if any. It is best-effort: a missing
// or unsaveable binding never fails registration itself.
func (c *cli) captureRuntimeSession(runtimeID string) {
	sessionID, adapter := sessionbind.Capture()
	if sessionID == "" {
		return
	}
	if err := sessionbind.Save(c.svc.Store.Root, runtimeID, sessionID, adapter); err != nil {
		return
	}
	if !c.quiet {
		_, _ = fmt.Fprintf(c.err, "captured %s session %s for runtime %s\n", cliui.SanitizeInline(adapter), cliui.SanitizeInline(sessionID), cliui.SanitizeInline(runtimeID))
	}
}

// errorCode/exitCode delegate entirely to internal/failure's shared
// classifier -- the same one MCP's rpcFail uses -- so
// *projectlifecycle.Error and every other classified error type is
// unwrapped in exactly one place, not duplicated per interface.
func errorCode(e error) string {
	return failure.Code(e)
}
func exitCode(e error) int {
	return failure.ExitStatus(e)
}
