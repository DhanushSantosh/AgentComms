package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/doctor"
	"github.com/DhanushSantosh/AgentComms/internal/failure"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
	"github.com/DhanushSantosh/AgentComms/internal/runtimeinit"
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
	r.AddCommand(c.versionCmd(), c.initCmd(), c.projectCmd(), c.doctorCmd(), c.verifyCmd(), c.statusCmd(), c.attentionCmd(), c.historyCmd(), c.agentCmd(), c.runtimeCmd(), c.invocationCmd(), c.sessionCmd(), c.taskCmd(), c.messageCmd(), c.decisionCmd(), c.approvalCmd(), c.artifactCmd(), c.documentCmd(), c.envCmd(), c.draftCmd(), c.archiveCmd(), c.exportCmd(), c.profileCmd(), c.configCmd(), c.updateCmd(), c.completionCmd(r), c.agentInstructionsCmd(), c.mcpCmd(), c.watchCmd(), c.tuiCmd(), c.daemonCmd(), c.liveCmd())
	configureRootHelp(r)
	return r
}

func configureRootHelp(root *cobra.Command) {
	groups := map[string]string{
		"version": "start", "init": "start", "status": "start", "doctor": "start", "verify": "start", "tui": "start",
		"task": "coordinate", "message": "coordinate", "decision": "coordinate", "approval": "coordinate", "invocation": "coordinate", "attention": "coordinate",
		"agent": "identity", "runtime": "identity", "session": "identity", "profile": "identity",
		"artifact": "knowledge", "document": "knowledge", "env": "knowledge", "draft": "knowledge", "archive": "knowledge", "history": "knowledge",
		"project": "operations", "config": "operations", "update": "operations", "watch": "operations", "export": "operations", "agent-instructions": "operations", "completion": "operations", "mcp": "operations", "live": "operations",
	}
	descriptions := map[string]string{
		"agent": "Manage governed identities, roles, scopes, and keys", "approval": "Request and resolve governed approvals",
		"archive": "Archive eligible completed project state", "artifact": "Store, inspect, and verify content-addressed artifacts",
		"attention":  "List everything currently needing operator intervention",
		"completion": "Generate shell completion source", "config": "Inspect and set user and project configuration",
		"decision": "Record and supersede durable decisions", "doctor": "Diagnose project health and actionable findings",
		"document": "Create and manage governed project documents", "env": "Manage governed project environment values",
		"export": "Export project history as JSONL or Markdown", "history": "Inspect and search the signed event timeline",
		"live":    "Serve, attach to, or tail a provider's live agent sessions",
		"message": "Post messages and manage recipient obligations", "mcp": "Serve the protocol-only MCP stdio interface",
		"profile": "Inspect and select local signing profiles",
		"session": "Manage durable invocation sessions", "status": "Show a concise project operational summary",
		"task": "Coordinate ownership, leases, handoffs, and task lifecycle", "tui": "Open the full-screen control room",
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
func (c *cli) emit(command string, v any, warnings ...string) error {
	return c.emitWithDelivery(command, v, nil, warnings...)
}

func (c *cli) emitStream(command, event string, data any) error {
	return json.NewEncoder(c.out).Encode(StreamEnvelope{
		APIVersion: APIVersion, Command: command, Event: event,
		Timestamp: time.Now().UTC(), Data: data,
	})
}

func (c *cli) emitDocument(command string, value any, document cliui.Document, warnings ...string) error {
	if c.json {
		return c.emit(command, value, warnings...)
	}
	if len(c.pendingWarnings) > 0 {
		warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
	}
	mode := cliui.Mode(c.output)
	if mode == "" {
		mode = cliui.ModeHuman
	}
	if c.quiet {
		return c.renderWarnings(mode, warnings)
	}
	if c.verbose {
		document.Fields = append(document.Fields,
			cliui.Field{Label: "Command", Value: c.cmd},
			cliui.Field{Label: "Output", Value: string(mode)},
			cliui.Field{Label: "Project", Value: c.project},
			cliui.Field{Label: "Actor", Value: c.actor},
		)
	}
	presenter := cliui.Presenter{
		Out:          c.out,
		Mode:         mode,
		Capabilities: cliui.DetectCapabilities(c.out, c.noColor),
	}
	if err := presenter.Render(document); err != nil {
		return err
	}
	if c.details {
		if err := presenter.RenderDetails(value); err != nil {
			return err
		}
	}
	return c.renderWarnings(mode, warnings)
}

func (c *cli) renderWarnings(mode cliui.Mode, warnings []string) error {
	presenter := cliui.Presenter{
		Out:          c.err,
		Mode:         mode,
		Capabilities: cliui.DetectCapabilities(c.err, c.noColor),
	}
	for _, warning := range warnings {
		if err := presenter.RenderWarning(warning); err != nil {
			return err
		}
	}
	return nil
}

func (c *cli) progress() *cliui.Progress {
	mode := cliui.Mode(c.output)
	if c.json {
		mode = cliui.ModeJSON
	}
	return cliui.NewProgress(c.err, mode, cliui.DetectCapabilities(c.err, c.noColor), c.quiet)
}

// emitTable is emit's counterpart for list-shaped output. JSON mode retains
// the same envelope and payload as emit. Human and plain modes use the shared,
// display-width-aware table presenter.
func (c *cli) emitTable(command string, v any, headers []string, rows [][]string, warnings ...string) error {
	if c.json || c.quiet {
		return c.emit(command, v, warnings...)
	}
	if len(c.pendingWarnings) > 0 {
		warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
	}
	mode := cliui.Mode(c.output)
	if mode == "" {
		mode = cliui.ModeHuman
	}
	if err := (cliui.Presenter{
		Out:          c.out,
		Mode:         mode,
		Capabilities: cliui.DetectCapabilities(c.out, c.noColor),
	}).RenderTable(cliui.Table{Headers: headers, Rows: rows}); err != nil {
		return err
	}
	presenter := cliui.Presenter{Out: c.out, Mode: mode, Capabilities: cliui.DetectCapabilities(c.out, c.noColor)}
	if c.verbose {
		if err := presenter.RenderSection("Operational context", map[string]any{"command": c.cmd, "project": c.project, "actor": c.actor}); err != nil {
			return err
		}
	}
	if c.details {
		if err := presenter.RenderDetails(v); err != nil {
			return err
		}
	}
	return c.renderWarnings(mode, warnings)
}

func (c *cli) emitTimeline(command string, value any, timeline cliui.Timeline, warnings ...string) error {
	if c.json {
		return c.emit(command, value, warnings...)
	}
	if c.quiet {
		if len(c.pendingWarnings) > 0 {
			warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
		}
		mode := cliui.Mode(c.output)
		if mode == "" {
			mode = cliui.ModeHuman
		}
		return c.renderWarnings(mode, warnings)
	}
	if len(c.pendingWarnings) > 0 {
		warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
	}
	mode := cliui.Mode(c.output)
	if mode == "" {
		mode = cliui.ModeHuman
	}
	presenter := cliui.Presenter{Out: c.out, Mode: mode, Capabilities: cliui.DetectCapabilities(c.out, c.noColor)}
	if err := presenter.RenderTimeline(timeline); err != nil {
		return err
	}
	if c.verbose {
		if err := presenter.RenderSection("Operational context", map[string]any{"command": c.cmd, "project": c.project, "actor": c.actor}); err != nil {
			return err
		}
	}
	if c.details {
		if err := presenter.RenderDetails(value); err != nil {
			return err
		}
	}
	return c.renderWarnings(mode, warnings)
}

// renderTable writes headers and rows as a plain-text table, columns
// padded to the widest cell in each column (header included), separated
// by two spaces. Deliberately no box-drawing characters: those need
// display-width handling for anything beyond plain ASCII to stay aligned,
// and this output is meant to copy-paste cleanly into another command or
// a message, which a bordered table doesn't do as well.
func renderTable(out io.Writer, headers []string, rows [][]string) {
	_ = (cliui.Presenter{Out: out, Mode: cliui.ModePlain}).RenderTable(cliui.Table{Headers: headers, Rows: rows})
}

func (c *cli) emitWithDelivery(command string, v, delivery any, warnings ...string) error {
	if c.json {
		if len(c.pendingWarnings) > 0 {
			warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
		}
		return json.NewEncoder(c.out).Encode(Envelope{
			APIVersion: APIVersion, OK: true, Command: command,
			Result: v, Delivery: delivery, Warnings: warnings,
		})
	}
	if c.quiet {
		if len(c.pendingWarnings) > 0 {
			warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
		}
		mode := cliui.Mode(c.output)
		if mode == "" {
			mode = cliui.ModeHuman
		}
		return c.renderWarnings(mode, warnings)
	}
	if event, ok := v.(model.Event); ok {
		return c.emitDocument(command, v, mutationReceipt(command, event, delivery), warnings...)
	}
	if len(c.pendingWarnings) > 0 {
		warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
	}
	mode := cliui.Mode(c.output)
	if mode == "" {
		mode = cliui.ModeHuman
	}
	presenter := cliui.Presenter{
		Out:          c.out,
		Mode:         mode,
		Capabilities: cliui.DetectCapabilities(c.out, c.noColor),
	}
	if e := presenter.RenderResult(command, v, delivery); e != nil {
		return e
	}
	if c.verbose {
		if e := presenter.RenderSection("Operational context", map[string]any{"command": c.cmd, "output": mode, "project": c.project, "actor": c.actor}); e != nil {
			return e
		}
	}
	return c.renderWarnings(mode, warnings)
}

func mutationReceipt(command string, event model.Event, delivery any) cliui.Document {
	parts := strings.Split(command, ".")
	domain, operation := "Operation", "completed"
	if len(parts) > 0 {
		domain = strings.ToUpper(parts[0][:1]) + parts[0][1:]
	}
	if len(parts) > 1 {
		operation = mutationVerb(parts[len(parts)-1])
	}
	fields := []cliui.Field{
		{Label: "Entity", Value: event.EntityID},
		{Label: "Actor", Value: event.Actor},
		{Label: "Event", Value: event.Type},
		{Label: "Sequence", Value: fmt.Sprint(event.Sequence)},
	}
	if event.Consistency != "" {
		fields = append(fields, cliui.Field{Label: "Consistency", Value: event.Consistency})
	}
	if event.KeyFingerprint != "" {
		fields = append(fields, cliui.Field{Label: "Signing key", Value: event.KeyFingerprint})
	}
	if outcome, ok := delivery.(service.InvocationDeliveryResult); ok {
		fields = append(fields,
			cliui.Field{Label: "Delivery", Value: outcome.Outcome},
			cliui.Field{Label: "Runtime", Value: outcome.RuntimeID},
		)
	}
	return cliui.Document{
		Title: domain + " " + operation, Status: cliui.StatusSuccess, Fields: fields,
		Hint: "Use --details for signed receipt metadata or --json for the stable machine envelope.",
	}
}

func mutationVerb(operation string) string {
	verbs := map[string]string{
		"create": "created", "register": "registered", "activate": "activated",
		"configure": "configured", "update": "updated", "set": "updated",
		"add": "added", "save": "saved", "post": "posted", "request": "requested",
		"offer": "offered", "claim": "claimed", "start": "started", "renew": "renewed",
		"complete": "completed", "resolve": "resolved", "approve": "approved",
		"reject": "rejected", "cancel": "cancelled", "delete": "deleted",
		"revoke": "revoked", "suspend": "suspended", "rename": "renamed",
		"supersede": "superseded", "takeover": "taken over", "handoff": "handed off",
		"heartbeat": "heartbeat recorded", "drain": "draining", "expire": "expired",
		"redeliver": "redelivered", "rotate-key": "key rotated", "elevate-key": "elevated key registered",
	}
	if verb := verbs[operation]; verb != "" {
		return verb
	}
	return strings.ReplaceAll(operation, "-", " ")
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
func (c *cli) versionCmd() *cobra.Command {
	return &cobra.Command{Use: "version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		result := map[string]any{"version": Version, "build_id": buildinfo.ResolvedBuildID(), "schema_version": model.SchemaVersion, "project_format_version": store.ProjectFormatVersion, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH}
		if c.json {
			return c.emit("version", result)
		}
		return c.emitDocument("version", result, cliui.Document{
			Title:  "Agent Comms",
			Status: cliui.StatusInfo,
			Fields: []cliui.Field{
				{Label: "Version", Value: Version},
				{Label: "Build", Value: buildinfo.ResolvedBuildID()},
				{Label: "Schema", Value: model.SchemaVersion},
				{Label: "Project format", Value: fmt.Sprint(store.ProjectFormatVersion)},
				{Label: "Runtime", Value: runtime.Version()},
				{Label: "Platform", Value: runtime.GOOS + "/" + runtime.GOARCH},
			},
		})
	}}
}

func (c *cli) projectCmd() *cobra.Command {
	root := &cobra.Command{Use: "project", Short: "Inspect and maintain the initialized project"}
	upgrade := c.projectUpgradeCmd()
	root.AddCommand(upgrade, c.projectDeleteCmd())
	return root
}

// projectDeleteCmd is RFC 0020's project teardown. Deliberately refuses
// outright under --non-interactive: matches agent elevate-key's own
// reasoning (see cmd_agent.go) -- a passphrase prompt is meaningless to a
// script, and this is the single most destructive command in the system.
// There is no --yes, no piped-passphrase flag; there is no scripted path
// for this one.
func (c *cli) projectDeleteCmd() *cobra.Command {
	return &cobra.Command{Use: "delete", Args: cobra.NoArgs,
		Short: "Permanently delete this project's local runtime, and its remote data in service mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			if c.nonInteractive {
				return errors.New("project delete requires an interactive terminal -- there is no non-interactive or scripted path for this command")
			}
			root := c.project
			if root == "" {
				var err error
				root, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			cfg, err := store.Open(root).Config()
			if err != nil {
				return err
			}
			directoryName := filepath.Base(root)
			fmt.Fprintf(c.out, "\nProject:      %s\n", cliui.SanitizeInline(cfg.ProjectID))
			fmt.Fprintf(c.out, "Directory:    %s\n", cliui.SanitizeInline(root))
			fmt.Fprintf(c.out, "Owner:        %s\n", cliui.SanitizeInline(cfg.Owner))
			fmt.Fprintf(c.out, "Runtime mode: %s\n", cliui.SanitizeInline(cfg.RuntimeMode))
			if cfg.RuntimeMode == "service" {
				fmt.Fprintf(c.out, "Authority:    %s\n", cliui.SanitizeInline(cfg.AuthorityURL))
				fmt.Fprint(c.out, "\nThis permanently deletes this project's ENTIRE row set from the shared authority above,\n"+
					"not just this machine's local copy -- every other member's access to it ends too.\n")
			}
			fmt.Fprint(c.out, "\nThis is IRREVERSIBLE. There is no backup.\n\n")
			fmt.Fprintf(c.out, "Type the project directory name (%s) to confirm permanent deletion: ", cliui.SanitizeInline(directoryName))
			scanner := bufio.NewScanner(os.Stdin)
			confirmed := ""
			if scanner.Scan() {
				confirmed = strings.TrimSpace(scanner.Text())
			}
			if confirmed != directoryName {
				return errors.New("project delete cancelled: typed confirmation did not match the project directory name")
			}
			passphrase, err := promptPassphrase(c.actor)
			if err != nil {
				return err
			}
			result, err := c.svc.DeleteProject(c.actor, passphrase, confirmed)
			if err != nil {
				return err
			}
			return c.emitDocument("project.delete", result, cliui.Document{
				Title: "Project deleted", Status: cliui.StatusDanger,
				Fields: []cliui.Field{{Label: "Project", Value: cfg.ProjectID}, {Label: "Directory", Value: root}, {Label: "Runtime mode", Value: cfg.RuntimeMode}},
			})
		},
	}
}

func (c *cli) projectUpgradeCmd() *cobra.Command {
	var yes, allKnown bool
	upgrade := &cobra.Command{
		Use:   "upgrade",
		Short: "Inspect, back up, reconcile, restart, and verify a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			roots, err := c.upgradeRoots(allKnown)
			if err != nil {
				return err
			}
			results := make([]projectlifecycle.Result, 0, len(roots))
			for _, root := range roots {
				plan, _, inspectErr := projectlifecycle.Inspect(root, Version, buildinfo.ResolvedBuildID())
				if inspectErr != nil {
					return inspectErr
				}
				approved := yes
				if plan.RequiresConfirmation && !approved {
					if c.nonInteractive {
						return &projectlifecycle.Error{Code: projectlifecycle.CodeUpgradeRequired, Message: "project upgrade requires --yes in non-interactive mode"}
					}
					fmt.Fprintf(c.out, "Upgrade %s with %d action(s)? [y/N] ", cliui.SanitizeInline(root), len(plan.Actions))
					scanner := bufio.NewScanner(os.Stdin)
					if !scanner.Scan() || !strings.EqualFold(strings.TrimSpace(scanner.Text()), "y") {
						return errors.New("project upgrade cancelled")
					}
					approved = true
				}
				result, reconcileErr := projectlifecycle.Reconcile(cmd.Context(), projectlifecycle.Options{
					Root: root, Version: Version, BuildID: buildinfo.ResolvedBuildID(),
					Apply: true, Approved: approved, Timeout: c.timeout, StopDaemon: true,
				})
				if reconcileErr != nil {
					return reconcileErr
				}
				projectService := service.New(root)
				config, configErr := projectService.Store.Config()
				if configErr != nil {
					return configErr
				}
				if config.RuntimeMode == "personal" || config.RuntimeMode == "service" {
					if daemonErr := ensureDaemon(root, config); daemonErr != nil {
						return daemonErr
					}
				}
				if verifyErr := projectService.Verify(0, 0); verifyErr != nil {
					return &projectlifecycle.Error{Code: projectlifecycle.CodeUpgradeFailed, Message: "post-upgrade audit verification: " + verifyErr.Error()}
				}
				result.Verified = true
				results = append(results, result)
			}
			if allKnown {
				if err = markUserInstallationCurrent(""); err != nil {
					return err
				}
			}
			result := map[string]any{
				"projects": results, "upgraded": countChangedProjects(results), "verified": true,
			}
			return c.emitDocument("project.upgrade", result, cliui.Document{
				Title: "Project upgrade completed", Status: cliui.StatusSuccess,
				Fields: []cliui.Field{{Label: "Projects", Value: fmt.Sprint(len(results))}, {Label: "Changed", Value: fmt.Sprint(countChangedProjects(results))}, {Label: "Verified", Value: "yes"}},
			})
		},
	}
	upgrade.Flags().BoolVarP(&yes, "yes", "y", false, "approve confirmation-required migrations")
	upgrade.Flags().BoolVar(&allKnown, "all-known", false, "upgrade distinct projects recorded in identity profiles")

	for _, operation := range []string{"status", "plan"} {
		operation := operation
		var operationAllKnown bool
		command := &cobra.Command{Use: operation, Short: "Show the pending project upgrade plan (" + operation + ")", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			roots, err := c.upgradeRoots(operationAllKnown)
			if err != nil {
				return err
			}
			plans := make([]projectlifecycle.Plan, 0, len(roots))
			for _, root := range roots {
				plan, _, inspectErr := projectlifecycle.Inspect(root, Version, buildinfo.ResolvedBuildID())
				if inspectErr != nil {
					return inspectErr
				}
				plans = append(plans, plan)
			}
			result := map[string]any{"projects": plans}
			actions, confirmations := 0, 0
			for _, plan := range plans {
				actions += len(plan.Actions)
				if plan.RequiresConfirmation {
					confirmations++
				}
			}
			return c.emitDocument("project.upgrade."+operation, result, cliui.Document{
				Title: "Project upgrade " + operation, Status: cliui.StatusInfo,
				Fields: []cliui.Field{{Label: "Projects", Value: fmt.Sprint(len(plans))}, {Label: "Actions", Value: fmt.Sprint(actions)}, {Label: "Confirmations", Value: fmt.Sprint(confirmations)}},
				Hint:   "Run project upgrade with --yes when confirmation-required actions are understood and approved.",
			})
		}}
		command.Flags().BoolVar(&operationAllKnown, "all-known", false, "inspect distinct projects recorded in identity profiles")
		upgrade.AddCommand(command)
	}
	return upgrade
}

func (c *cli) upgradeRoots(allKnown bool) ([]string, error) {
	root := c.project
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	if !allKnown {
		return []string{root}, nil
	}
	// Fold in the current project root even if it isn't registered in
	// identity profiles, matching reconcileUserInstallation and update
	// apply's handoff -- otherwise "--all-known" could silently exclude
	// the very project the user is standing in (e.g. a moved directory
	// whose stored profile.ProjectRoot no longer matches, or a profile
	// write that failed to save).
	roots, err := c.knownProjectRoots(root)
	if err != nil {
		return nil, err
	}
	initialized := roots[:0]
	for _, root := range roots {
		if initializedProject(root) {
			initialized = append(initialized, root)
		}
	}
	roots = initialized
	if len(roots) == 0 {
		return nil, errors.New("no initialized projects are recorded in identity profiles")
	}
	return roots, nil
}

func countChangedProjects(results []projectlifecycle.Result) int {
	count := 0
	for _, result := range results {
		if result.Changed {
			count++
		}
	}
	return count
}
func (c *cli) initCmd() *cobra.Command {
	var owner string
	var mode string
	var yes bool
	var authorityURL, servicePublicKey, daemonEndpoint string
	cmd := &cobra.Command{Use: "init", Short: "Initialize an Agent Comms project", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		root := c.project
		if root == "" {
			root, _ = os.Getwd()
		}
		if owner == "" && !c.nonInteractive {
			fmt.Fprint(c.out, "Owner identity [owner]: ")
			scan := bufio.NewScanner(os.Stdin)
			if scan.Scan() {
				owner = strings.TrimSpace(scan.Text())
			}
			if owner == "" {
				owner = "owner"
			}
		}
		if owner == "" {
			return errors.New("--owner is required in non-interactive mode")
		}
		if mode != "personal" && mode != "service" {
			return errors.New("--mode must be personal or service")
		}
		if !yes && !c.nonInteractive {
			fmt.Fprintf(c.out, "\nCreate .agents and isolated .agent-comms runtime in %s? [y/N] ", cliui.SanitizeInline(root))
			scan := bufio.NewScanner(os.Stdin)
			if !scan.Scan() || !strings.EqualFold(strings.TrimSpace(scan.Text()), "y") {
				return errors.New("initialization cancelled")
			}
		}
		initialized, e := runtimeinit.Initialize(cmd.Context(), runtimeinit.Config{
			ProjectRoot: root, Owner: owner, Mode: mode, AuthorityURL: authorityURL,
			AuthorityToken:   strings.TrimSpace(os.Getenv("AGENT_COMMS_AUTHORITY_TOKEN")),
			ServicePublicKey: servicePublicKey, DaemonEndpoint: daemonEndpoint,
		})
		if e != nil {
			return e
		}
		result := map[string]any{
			"project": root, "runtime": filepath.Join(root, store.Runtime), "owner": owner,
			"next":         []string{"agent-comms tui", "agent-comms agent register --id reviewer --principal-type AGENT"},
			"runtime_mode": initialized.RuntimeMode, "daemon_endpoint": initialized.DaemonEndpoint,
		}
		if initialized.Database != "" {
			result["database"] = initialized.Database
		}
		if initialized.AuthorityURL != "" {
			result["authority_url"] = initialized.AuthorityURL
		}
		// Offered here, not left for the owner to discover later, so it's
		// never silently invisible: this project can only be used after
		// init, so init is the one moment guaranteed to reach every human
		// owner. Skipped in --non-interactive mode (scripts, CI, tests) --
		// `doctor`'s NO_ELEVATED_KEY finding (see below) is what resurfaces
		// this for a project that declined or couldn't answer here.
		if !c.nonInteractive {
			fmt.Fprint(c.out, "\nSet up a passphrase-protected elevated key now? It's required to grant ORCHESTRATOR "+
				"and approve HUMAN-tier approvals -- without it those stay protected only by ordinary credential "+
				"possession. [Y/n] ")
			scan := bufio.NewScanner(os.Stdin)
			answer := ""
			if scan.Scan() {
				answer = strings.TrimSpace(scan.Text())
			}
			if answer == "" || strings.EqualFold(answer, "y") {
				svc := service.New(root)
				svc.PassphrasePrompt = promptPassphrase
				// init never goes through PersistentPreRunE (it's exempted --
				// the project doesn't exist yet when that runs), so unlike
				// every other command nothing has started the daemon this
				// runtime needs for ElevateKey's signed command. Wire the
				// same on-demand recovery PersistentPreRunE sets up for
				// ordinary commands rather than requiring one to already be
				// running.
				if svcCfg, cfgErr := svc.Store.Config(); cfgErr == nil {
					svc.SetRemoteRecovery(func() error { return ensureDaemon(root, svcCfg) })
				}
				passphrase, e := promptNewPassphrase(owner)
				if e == nil {
					_, e = svc.ElevateKey(owner, passphrase)
				}
				if e != nil {
					// Non-fatal: the project itself is already created and
					// fully usable, so failing the whole `init` command here
					// would misrepresent what happened. `doctor`'s
					// NO_ELEVATED_KEY finding is the real safety net for
					// this path -- it nags on every future command until
					// `agent-comms agent elevate-key` is run.
					fmt.Fprintf(c.out, "elevated-key setup failed (%s); run `agent-comms agent elevate-key` to finish this later\n", cliui.SanitizeInline(e.Error()))
					result["elevated_key"] = "skipped: " + e.Error()
				} else {
					result["elevated_key"] = "registered"
				}
			} else {
				result["elevated_key"] = "skipped"
			}
		}
		fields := []cliui.Field{
			{Label: "Project", Value: root}, {Label: "Owner", Value: owner},
			{Label: "Runtime mode", Value: initialized.RuntimeMode}, {Label: "Daemon", Value: initialized.DaemonEndpoint},
		}
		if initialized.AuthorityURL != "" {
			fields = append(fields, cliui.Field{Label: "Authority", Value: initialized.AuthorityURL})
		}
		return c.emitDocument("init", result, cliui.Document{
			Title: "Project initialized", Status: cliui.StatusSuccess, Fields: fields,
			Hint: "Open the control room with agent-comms tui or register the first collaborating agent.",
		})
	}}
	cmd.Flags().StringVar(&owner, "owner", "", "owner principal ID")
	cmd.Flags().StringVar(&mode, "mode", "personal", "runtime mode: personal or service")
	cmd.Flags().StringVar(&authorityURL, "authority-url", "", "service authority URL")
	cmd.Flags().StringVar(&servicePublicKey, "service-public-key", "", "base64 Ed25519 service public key")
	cmd.Flags().StringVar(&daemonEndpoint, "daemon-endpoint", "", "local daemon socket or named pipe")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "confirm initialization")
	return cmd
}
func (c *cli) doctorCmd() *cobra.Command {
	var explain bool
	cmd := &cobra.Command{Use: "doctor", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		// projectOptional (RFC 0027 section 12): no initialized project in
		// this directory. Report that as a finding rather than erroring.
		if c.svc == nil {
			result := map[string]any{
				"healthy": false,
				"findings": []doctor.Finding{{
					Severity: "WARNING", Code: "NO_PROJECT_HERE",
					Message:  "no initialized Agent Comms project in this directory",
					Guidance: "Run `agent-comms init` here, or cd into an existing project.",
				}},
			}
			return c.emitDocument("doctor", result, cliui.Document{
				Title: "Project health", Status: cliui.StatusWarning,
				Fields: []cliui.Field{{Label: "Findings", Value: "WARNING NO_PROJECT_HERE — no initialized Agent Comms project in this directory. Remedy: run `agent-comms init` here, or cd into an existing project."}},
			})
		}
		verify := c.svc.Verify(0, 0)
		cfg, e := c.svc.Store.Config()
		if e != nil {
			return e
		}
		findings, e := doctor.Findings(cmd.Context(), c.svc)
		if e != nil {
			return e
		}
		add := func(severity, code, message, guidance string) {
			findings = append(findings, doctor.Finding{Severity: severity, Code: code, Message: message, Guidance: guidance})
		}
		lifecycle, _, lifecycleErr := projectlifecycle.Inspect(c.svc.Store.Root, Version, buildinfo.ResolvedBuildID())
		if lifecycleErr != nil {
			add("ERROR", "PROJECT_LIFECYCLE_INVALID", lifecycleErr.Error(), "Run `agent-comms project upgrade status` and repair the reported compatibility problem.")
		} else if len(lifecycle.Actions) > 0 || lifecycle.Interrupted {
			add("WARNING", "PROJECT_UPGRADE_AVAILABLE",
				fmt.Sprintf("project has %d lifecycle action(s); interrupted=%t", len(lifecycle.Actions), lifecycle.Interrupted),
				"Run `agent-comms project upgrade`; it plans, backs up, resumes, and verifies the project in one operation.")
		}
		r := map[string]any{"integrity": verify == nil, "schema_version": cfg.SchemaVersion, "binary_version": Version, "binary_build_id": buildinfo.ResolvedBuildID(), "runtime_toolkit_version": cfg.ToolkitVersion, "runtime_toolkit_build_id": cfg.ToolkitBuildID, "project_format_version": cfg.ProjectFormatVersion, "managed_files_version": cfg.ManagedFilesVersion, "runtime": filepath.Join(c.svc.Store.Root, store.Runtime), "telemetry": false, "healthy": len(findings) == 0, "findings": findings, "project_lifecycle": lifecycle}
		if cfg.RuntimeMode == "service" || cfg.RuntimeMode == "personal" {
			r["runtime_mode"] = cfg.RuntimeMode
			if cfg.RuntimeMode == "service" {
				r["authority_url"] = cfg.AuthorityURL
			} else {
				r["database"] = runtimeinit.DatabasePath(c.svc.Store.Root)
			}
		}
		if verify != nil {
			r["integrity_error"] = verify.Error()
		}
		if explain {
			r["config_sources"] = []string{"CLI flags", "AGENT_COMMS_* environment", "project .agent-comms/config.json", "user config", "built-in defaults"}
			r["resolved_lock_timeout"] = c.timeout.String()
			r["actor"] = c.actor
		}
		status := cliui.StatusSuccess
		if len(findings) > 0 || verify != nil {
			status = cliui.StatusWarning
		}
		findingSummary := "None"
		if len(findings) > 0 {
			items := make([]string, 0, len(findings))
			for _, finding := range findings {
				detail := fmt.Sprintf("%s %s — %s", finding.Severity, finding.Code, finding.Message)
				if finding.Guidance != "" {
					detail += " Remedy: " + finding.Guidance
				}
				items = append(items, detail)
			}
			findingSummary = strings.Join(items, "; ")
		}
		integrityStatus := "verified"
		if verify != nil {
			integrityStatus = "failed: " + verify.Error()
		}
		return c.emitDocument("doctor", r, cliui.Document{
			Title:  "Project health",
			Status: status,
			Fields: []cliui.Field{
				{Label: "Integrity", Value: integrityStatus},
				{Label: "Findings", Value: findingSummary},
				{Label: "Runtime mode", Value: cfg.RuntimeMode},
				{Label: "Schema", Value: cfg.SchemaVersion},
				{Label: "Binary", Value: Version + " (" + buildinfo.ResolvedBuildID() + ")"},
			},
		})
	}}
	cmd.Flags().BoolVar(&explain, "explain-config", false, "show configuration precedence and resolved values")
	return cmd
}
func (c *cli) verifyCmd() *cobra.Command {
	var from, to uint64
	cmd := &cobra.Command{Use: "verify", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if e := c.svc.Verify(from, to); e != nil {
			return e
		}
		state, e := c.svc.State()
		if e != nil {
			return e
		}
		result := map[string]any{
			"verified": true, "events": state.Integrity.EventCount, "head": state.Integrity.Head,
			"from": from, "to": to, "consistency": state.Integrity.Consistency,
			"server_sequence": state.Integrity.ServerSequence, "cache_sequence": state.Integrity.CacheSequence,
			"connectivity": state.Integrity.Connectivity,
		}
		return c.emitDocument("verify", result, cliui.Document{
			Title:  "Integrity verified",
			Status: cliui.StatusSuccess,
			Fields: []cliui.Field{
				{Label: "Events", Value: fmt.Sprint(state.Integrity.EventCount)},
				{Label: "Consistency", Value: state.Integrity.Consistency},
				{Label: "Connectivity", Value: state.Integrity.Connectivity},
				{Label: "Head", Value: state.Integrity.Head},
			},
		})
	}}
	cmd.Flags().Uint64Var(&from, "from", 0, "first event sequence to verify")
	cmd.Flags().Uint64Var(&to, "to", 0, "last event sequence to verify (zero means current head)")
	return cmd
}
func (c *cli) statusCmd() *cobra.Command {
	return &cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		v, e := c.svc.State()
		if e != nil {
			return e
		}
		onlineRuntimes := 0
		for _, runtime := range v.AgentRuntimes {
			if runtime.Status == "ONLINE" {
				onlineRuntimes++
			}
		}
		pendingApprovals := 0
		for _, approval := range v.Approvals {
			if approval.Status == "PENDING" {
				pendingApprovals++
			}
		}
		integrity := "not verified"
		status := cliui.StatusWarning
		if v.Integrity.Verified {
			integrity = "verified"
			status = cliui.StatusSuccess
		}
		breakdown := map[string]int{
			"online_runtimes": onlineRuntimes, "draining_runtimes": 0, "pending_approvals": pendingApprovals,
		}
		for _, runtime := range v.AgentRuntimes {
			if runtime.Status == "DRAINING" {
				breakdown["draining_runtimes"]++
			}
		}
		for _, invocation := range v.Invocations {
			breakdown["invocations_"+strings.ToLower(invocation.Status)]++
		}
		for _, task := range v.Tasks {
			breakdown["tasks_"+strings.ToLower(task.Status)]++
		}
		// Keep the State's own fields at the top level of the result for
		// --json contract stability (RFC 0022); add the per-status
		// breakdown alongside them for `status --details` (RFC 0027 §2).
		result := map[string]any{}
		if raw, marshalErr := json.Marshal(v); marshalErr == nil {
			_ = json.Unmarshal(raw, &result)
		}
		result["breakdown"] = breakdown
		return c.emitDocument("status", result, cliui.Document{
			Title:  "Project status",
			Status: status,
			Fields: []cliui.Field{
				{Label: "Agents", Value: fmt.Sprint(len(v.Agents))},
				{Label: "Runtimes", Value: fmt.Sprintf("%d (%d online)", len(v.AgentRuntimes), onlineRuntimes)},
				{Label: "Tasks", Value: fmt.Sprint(len(v.Tasks))},
				{Label: "Invocations", Value: fmt.Sprint(len(v.Invocations))},
				{Label: "Approvals", Value: fmt.Sprintf("%d (%d pending)", len(v.Approvals), pendingApprovals)},
				{Label: "Integrity", Value: integrity + " · " + v.Integrity.Consistency},
			},
			Hint: "Use --details for the per-status task, invocation, and runtime breakdown.",
		})
	}}
}

// attentionCmd is RFC 0027 section 2: the former `control attention`,
// promoted to a top-level command. It lists everything currently needing
// operator intervention as a categorized snapshot (distinct from `watch`,
// which streams changes).
func (c *cli) attentionCmd() *cobra.Command {
	return &cobra.Command{Use: "attention", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		state, err := c.svc.State()
		if err != nil {
			return err
		}
		blockedTasks := map[string]model.Task{}
		pendingApprovals := map[string]model.Approval{}
		waitingInvocations := map[string]model.Invocation{}
		failedDeliveries := map[string]model.InvocationDelivery{}
		degradedRuntimes := map[string]model.AgentRuntime{}
		for id, task := range state.Tasks {
			if task.Status == "BLOCKED" {
				blockedTasks[id] = task
			}
		}
		for id, approval := range state.Approvals {
			if approval.Status == "PENDING" {
				pendingApprovals[id] = approval
			}
		}
		for id, invocation := range state.Invocations {
			if invocation.Status == "WAITING" {
				waitingInvocations[id] = invocation
			}
		}
		for id, delivery := range state.InvocationDeliveries {
			if delivery.Status == "FAILED" || delivery.Status == "EXHAUSTED" {
				failedDeliveries[id] = delivery
			}
		}
		for id, runtime := range state.AgentRuntimes {
			if runtime.Health == "DEGRADED" || runtime.Status == "REVOKED" {
				degradedRuntimes[id] = runtime
			}
		}
		result := map[string]any{
			"blocked_tasks": blockedTasks, "pending_approvals": pendingApprovals,
			"waiting_invocations": waitingInvocations, "failed_deliveries": failedDeliveries,
			"degraded_runtimes": degradedRuntimes,
		}
		total := len(blockedTasks) + len(pendingApprovals) + len(waitingInvocations) + len(failedDeliveries) + len(degradedRuntimes)
		status := cliui.StatusSuccess
		if total > 0 {
			status = cliui.StatusWarning
		}
		return c.emitDocument("attention", result, cliui.Document{
			Title: "Attention queue", Status: status,
			Fields: []cliui.Field{
				{Label: "Blocked tasks", Value: fmt.Sprint(len(blockedTasks))}, {Label: "Pending approvals", Value: fmt.Sprint(len(pendingApprovals))},
				{Label: "Waiting invocations", Value: fmt.Sprint(len(waitingInvocations))}, {Label: "Failed deliveries", Value: fmt.Sprint(len(failedDeliveries))},
				{Label: "Degraded runtimes", Value: fmt.Sprint(len(degradedRuntimes))},
			},
			Hint: "Use --details to inspect every item requiring intervention.",
		})
	}}
}
func (c *cli) historyCmd() *cobra.Command {
	var cursor string
	var limit int
	var actor string
	var keyFingerprint string
	var grep string
	var all bool
	cmd := &cobra.Command{Use: "history", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		v, e := c.svc.History(controlplane.PageRequest{Cursor: cursor, Limit: limit})
		if e != nil {
			return e
		}
		// --all pages through the whole log rather than a single page, so
		// --grep is a real search and not "grep of the current page" the
		// removed `search` command was -- see RFC 0027 section 3.
		if all {
			for v.NextCursor != "" {
				next, pageErr := c.svc.History(controlplane.PageRequest{Cursor: v.NextCursor, Limit: limit})
				if pageErr != nil {
					return pageErr
				}
				v.Items = append(v.Items, next.Items...)
				v.NextCursor = next.NextCursor
			}
		}
		if grep != "" {
			needle := strings.ToLower(grep)
			matched := make([]controlplane.EventRecord, 0, len(v.Items))
			for _, record := range v.Items {
				blob, _ := json.Marshal(record)
				if strings.Contains(strings.ToLower(string(blob)), needle) {
					matched = append(matched, record)
				}
			}
			v.Items = matched
		}
		if actor != "" || keyFingerprint != "" {
			filtered := make([]controlplane.EventRecord, 0, len(v.Items))
			for _, record := range v.Items {
				if actor != "" && record.Event.Actor != actor {
					continue
				}
				if keyFingerprint != "" && record.Event.ActorKeyFingerprint != keyFingerprint {
					continue
				}
				filtered = append(filtered, record)
			}
			v.Items = filtered
		}
		entries := make([]cliui.TimelineEntry, 0, len(v.Items))
		for _, record := range v.Items {
			detail := record.Event.Actor
			if record.Event.EntityID != "" {
				detail += " · " + record.Event.EntityID
			}
			entries = append(entries, cliui.TimelineEntry{
				Time: record.Event.Time.Format(time.RFC3339), Status: cliui.StatusInfo,
				Title: fmt.Sprintf("#%d %s", record.Event.Sequence, record.Event.Type), Detail: detail,
			})
		}
		return c.emitTimeline("history", v, cliui.Timeline{Title: "Project history", Entries: entries})
	}}
	cmd.Flags().StringVar(&cursor, "cursor", "", "opaque pagination cursor")
	cmd.Flags().IntVar(&limit, "limit", controlplane.DefaultPageSize, "events per page")
	cmd.Flags().StringVar(&actor, "actor", "", "only events signed by this actor")
	cmd.Flags().StringVar(&keyFingerprint, "key-fingerprint", "", "only events signed by this key fingerprint")
	cmd.Flags().StringVar(&grep, "grep", "", "only events whose record contains this substring (case-insensitive)")
	cmd.Flags().BoolVar(&all, "all", false, "page through the entire log rather than one page")
	return cmd
}

// entityShow is RFC 0027 section 9: a uniform `show` for domains that
// previously only had `list`. lookup returns the entity, its summary
// fields, and whether it was found.
func (c *cli) entityShow(domain string, lookup func(model.State, string) (any, []cliui.Field, bool)) *cobra.Command {
	cmd := &cobra.Command{Use: "show", Args: cobra.NoArgs, Short: "Show one " + domain + " by ID", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		value, fields, ok := lookup(st, id)
		if !ok {
			return fmt.Errorf("%s %q not found", domain, id)
		}
		return c.emitDocument(domain+".show", value, cliui.Document{
			Title: strings.ToUpper(domain[:1]) + domain[1:] + " " + id, Status: cliui.StatusInfo, Fields: fields,
		})
	}}
	cmd.Flags().String("id", "", "entity ID")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func simpleStatus(c *cli, domain, sub string) *cobra.Command {
	return payloadStatus(c, domain, sub, func(string) any { return model.TaskStatus{} })
}
func statusWithSummary(c *cli, domain, sub string) *cobra.Command {
	var summary string
	cmd := payloadStatus(c, domain, sub, func(string) any { return model.TaskStatus{Summary: summary} })
	cmd.Flags().StringVar(&summary, "summary", "", "summary")
	return cmd
}

// transitionShort generates the one-line help for the many
// payloadStatus-built lifecycle transitions ("Move this task to
// blocked", "Reject this invocation", ...).
func transitionShort(domain, sub string) string {
	verb := strings.ReplaceAll(sub, "-", " ")
	switch sub {
	case "block", "review", "complete", "cancel", "start", "resume", "expire", "reject", "takeover", "approve":
		return "Move this " + domain + " to " + verb
	case "renew":
		return "Renew this " + domain + "'s lease with progress"
	case "claim":
		return "Claim this " + domain
	default:
		return strings.ToUpper(verb[:1]) + verb[1:] + " this " + domain
	}
}

func payloadStatus(c *cli, domain, sub string, f func(string) any) *cobra.Command {
	cmd := &cobra.Command{Use: sub, Short: transitionShort(domain, sub), RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		v, e := c.svc.Execute(c.actor, domain+"."+sub, id, f(id))
		if e != nil {
			return e
		}
		return c.emit(domain+"."+sub, v)
	}}
	cmd.Flags().String("id", "", "entity ID")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
