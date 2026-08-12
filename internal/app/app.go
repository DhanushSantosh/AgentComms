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
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}
type ExitError struct {
	Code int
	Kind string
	Err  error
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
	out, err                             io.Writer
	json, nonInteractive, noColor, quiet bool
	project, profile, actor              string
	timeout                              time.Duration
	svc                                  *service.Service
	cmd                                  string
	actorResolution                      identity.ActorResolution
	pendingWarnings                      []string
	processExitCode                      int
	handoffRunner                        commandRunner
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
		if c.json || ContainsJSONFlag(args) {
			body := Envelope{APIVersion: APIVersion, OK: false, Command: c.cmd, Error: &ErrorBody{Code: errorCode(e), Message: e.Error()}}
			_ = json.NewEncoder(stderr).Encode(body)
		}
		return &ExitError{Code: exitCode(e), Kind: errorCode(e), Err: e}
	}
	if c.processExitCode != 0 {
		err := fmt.Errorf("wrapped process exited with status %d", c.processExitCode)
		return &ExitError{Code: c.processExitCode, Kind: "PROCESS_EXIT", Err: err}
	}
	return nil
}
func (c *cli) root() *cobra.Command {
	r := &cobra.Command{Use: "agent-comms", Short: "Governed coordination for concurrent agents", Version: Version, PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		c.cmd = cmd.CommandPath()
		if cmd.Name() == "version" || cmd.Name() == "init" || cmd.Name() == "completion" ||
			(cmd.Name() == "update" && cmd.Parent() == cmd.Root()) ||
			strings.HasPrefix(cmd.CommandPath(), "agent-comms project upgrade") ||
			cmd.CommandPath() == "agent-comms daemon serve" ||
			cmd.CommandPath() == "agent-comms claude serve" ||
			cmd.CommandPath() == "agent-comms claude attach" ||
			cmd.CommandPath() == "agent-comms codex serve" ||
			cmd.CommandPath() == "agent-comms codex attach" ||
			cmd.CommandPath() == "agent-comms runtime verify-adapter" {
			return nil
		}
		if cmd.CommandPath() == "agent-comms profile list" ||
			cmd.CommandPath() == "agent-comms profile use" {
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
		applyLifecycle := cmd.Name() != "doctor"
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
		if cmd.Name() == "doctor" {
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
		if cmd.Name() != "doctor" && (cfg.RuntimeMode == "service" || cfg.RuntimeMode == "personal") {
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
	f := r.PersistentFlags()
	f.StringVar(&c.project, "project", "", "target project root")
	f.StringVar(&c.profile, "profile", "", "user profile name")
	f.StringVar(&c.actor, "actor", "", "actor override (credential must match)")
	f.BoolVar(&c.json, "json", false, "emit a versioned JSON envelope")
	f.BoolVar(&c.nonInteractive, "non-interactive", false, "never prompt")
	f.DurationVar(&c.timeout, "timeout", 10*time.Second, "transaction lock timeout")
	f.BoolVar(&c.noColor, "no-color", false, "disable ANSI color")
	f.BoolVarP(&c.quiet, "quiet", "q", false, "suppress non-essential output")
	r.AddCommand(c.versionCmd(), c.initCmd(), c.projectCmd(), c.doctorCmd(), c.verifyCmd(), c.statusCmd(), c.controlCmd(), c.historyCmd(), c.searchCmd(), c.agentCmd(), c.runtimeCmd(), c.invocationCmd(), c.sessionCmd(), c.taskCmd(), c.messageCmd(), c.decisionCmd(), c.approvalCmd(), c.artifactCmd(), c.documentCmd(), c.envCmd(), c.draftCmd(), c.archiveCmd(), c.exportCmd(), c.profileCmd(), c.configCmd(), c.themeCmd(), c.updateCmd(), c.completionCmd(r), c.agentInstructionsCmd(), c.mcpCmd(), c.watchCmd(), c.tuiCmd(), c.daemonCmd(), c.claudeCmd(), c.codexCmd())
	return r
}
func (c *cli) emit(command string, v any, warnings ...string) error {
	return c.emitWithDelivery(command, v, nil, warnings...)
}

// emitTable is emit's counterpart for list-shaped output. With --json (or
// scripting against this command generally), behavior is byte-for-byte
// identical to emit(command, v) -- the same JSON envelope, so nothing that
// already parses this command's output breaks. Only the human-facing,
// non-JSON default differs: emit's own non-JSON fallback still prints
// pretty-*indented* JSON, not an actual table -- a human doing an ad hoc
// check has to mentally parse it either way. Confirmed friction all
// session: reading agent/runtime/invocation state by eye meant piping
// through `python3 -m json.tool`/`jq` every single time, for exactly this
// reason. emitTable renders headers/rows as an aligned plain-text table
// instead.
func (c *cli) emitTable(command string, v any, headers []string, rows [][]string, warnings ...string) error {
	if c.json || c.quiet {
		return c.emit(command, v, warnings...)
	}
	if len(c.pendingWarnings) > 0 {
		warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
	}
	renderTable(c.out, headers, rows)
	for _, w := range warnings {
		if _, e := fmt.Fprintln(c.err, "warning:", w); e != nil {
			return e
		}
	}
	return nil
}

// renderTable writes headers and rows as a plain-text table, columns
// padded to the widest cell in each column (header included), separated
// by two spaces. Deliberately no box-drawing characters: those need
// display-width handling for anything beyond plain ASCII to stay aligned,
// and this output is meant to copy-paste cleanly into another command or
// a message, which a bordered table doesn't do as well.
func renderTable(out io.Writer, headers []string, rows [][]string) {
	if len(rows) == 0 {
		fmt.Fprintln(out, "(no rows)")
		return
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	writeRow := func(cells []string) {
		parts := make([]string, len(headers))
		for i := range headers {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			parts[i] = cell + strings.Repeat(" ", widths[i]-len(cell))
		}
		fmt.Fprintln(out, strings.TrimRight(strings.Join(parts, "  "), " "))
	}
	writeRow(headers)
	for _, row := range rows {
		writeRow(row)
	}
}

func (c *cli) emitWithDelivery(command string, v, delivery any, warnings ...string) error {
	if len(c.pendingWarnings) > 0 {
		warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
	}
	if c.json {
		return json.NewEncoder(c.out).Encode(Envelope{
			APIVersion: APIVersion, OK: true, Command: command,
			Result: v, Delivery: delivery, Warnings: warnings,
		})
	}
	if c.quiet {
		return nil
	}
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if _, e = fmt.Fprintln(c.out, string(b)); e != nil {
		return e
	}
	if delivery != nil {
		deliveryBody, marshalErr := json.MarshalIndent(delivery, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		if _, e = fmt.Fprintln(c.out, "delivery:", string(deliveryBody)); e != nil {
			return e
		}
	}
	for _, w := range warnings {
		if _, e = fmt.Fprintln(c.err, "warning:", w); e != nil {
			return e
		}
	}
	return nil
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
		_, _ = fmt.Fprintf(c.err, "captured %s session %s for runtime %s\n", adapter, sessionID, runtimeID)
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
		return c.emit("version", map[string]any{"version": Version, "build_id": buildinfo.ResolvedBuildID(), "schema_version": model.SchemaVersion, "project_format_version": store.ProjectFormatVersion, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH})
	}}
}

func (c *cli) projectCmd() *cobra.Command {
	root := &cobra.Command{Use: "project", Short: "Inspect and maintain the initialized project"}
	upgrade := c.projectUpgradeCmd()
	root.AddCommand(upgrade)
	return root
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
					fmt.Fprintf(c.out, "Upgrade %s with %d action(s)? [y/N] ", root, len(plan.Actions))
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
			return c.emit("project.upgrade", map[string]any{
				"projects": results, "upgraded": countChangedProjects(results), "verified": true,
			})
		},
	}
	upgrade.Flags().BoolVarP(&yes, "yes", "y", false, "approve confirmation-required migrations")
	upgrade.Flags().BoolVar(&allKnown, "all-known", false, "upgrade distinct projects recorded in identity profiles")

	for _, operation := range []string{"status", "plan"} {
		operation := operation
		var operationAllKnown bool
		command := &cobra.Command{Use: operation, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
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
			return c.emit("project.upgrade."+operation, map[string]any{"projects": plans})
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
			fmt.Fprintf(c.out, "\nCreate .agents and isolated .agent-comms runtime in %s? [y/N] ", root)
			scan := bufio.NewScanner(os.Stdin)
			if !scan.Scan() || !strings.EqualFold(strings.TrimSpace(scan.Text()), "y") {
				return errors.New("initialization cancelled")
			}
		}
		initialized, e := runtimeinit.Initialize(cmd.Context(), runtimeinit.Config{
			ProjectRoot: root, Owner: owner, Mode: mode, AuthorityURL: authorityURL,
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
					fmt.Fprintf(c.out, "elevated-key setup failed (%v); run `agent-comms agent elevate-key` to finish this later\n", e)
					result["elevated_key"] = "skipped: " + e.Error()
				} else {
					result["elevated_key"] = "registered"
				}
			} else {
				result["elevated_key"] = "skipped"
			}
		}
		return c.emit("init", result)
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
		return c.emit("doctor", r)
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
		return c.emit("verify", map[string]any{
			"verified": true, "events": state.Integrity.EventCount, "head": state.Integrity.Head,
			"from": from, "to": to, "consistency": state.Integrity.Consistency,
			"server_sequence": state.Integrity.ServerSequence, "cache_sequence": state.Integrity.CacheSequence,
			"connectivity": state.Integrity.Connectivity,
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
		return c.emit("status", v)
	}}
}
func (c *cli) controlCmd() *cobra.Command {
	root := &cobra.Command{Use: "control", Short: "Inspect the human project control plane"}
	overview := &cobra.Command{Use: "overview", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		state, err := c.svc.State()
		if err != nil {
			return err
		}
		counts := map[string]int{
			"agents": len(state.Agents), "runtimes": len(state.AgentRuntimes),
			"tasks": len(state.Tasks), "invocations": len(state.Invocations),
			"online_runtimes": 0, "draining_runtimes": 0, "pending_approvals": 0,
		}
		for _, runtime := range state.AgentRuntimes {
			if runtime.Status == "ONLINE" {
				counts["online_runtimes"]++
			}
			if runtime.Status == "DRAINING" {
				counts["draining_runtimes"]++
			}
		}
		for _, invocation := range state.Invocations {
			counts["invocations_"+strings.ToLower(invocation.Status)]++
		}
		for _, task := range state.Tasks {
			counts["tasks_"+strings.ToLower(task.Status)]++
		}
		for _, approval := range state.Approvals {
			if approval.Status == "PENDING" {
				counts["pending_approvals"]++
			}
		}
		return c.emit("control.overview", map[string]any{
			"counts": counts, "integrity": state.Integrity,
		})
	}}
	attention := &cobra.Command{Use: "attention", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
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
		return c.emit("control.attention", map[string]any{
			"blocked_tasks": blockedTasks, "pending_approvals": pendingApprovals,
			"waiting_invocations": waitingInvocations, "failed_deliveries": failedDeliveries,
			"degraded_runtimes": degradedRuntimes,
		})
	}}
	settings := &cobra.Command{Use: "settings", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		state, err := c.svc.State()
		if err != nil {
			return err
		}
		config, err := c.svc.Store.Config()
		if err != nil {
			return err
		}
		return c.emit("control.settings", map[string]any{
			"runtime_mode": config.RuntimeMode, "authority_url": config.AuthorityURL,
			"invocation_policies": state.InvocationPolicies,
			"limits": map[string]any{
				"max_runtime_concurrency": controlplane.MaxRuntimeConcurrency,
				"max_delivery_attempts":   controlplane.MaxDeliveryAttempts,
				"max_invocation_bytes":    controlplane.MaxInvocationBytes,
				"max_invocation_ttl":      controlplane.MaxInvocationTTL.String(),
			},
		})
	}}
	root.AddCommand(overview, attention, settings)
	return root
}
func (c *cli) historyCmd() *cobra.Command {
	var cursor string
	var limit int
	var actor string
	var keyFingerprint string
	cmd := &cobra.Command{Use: "history", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		v, e := c.svc.History(controlplane.PageRequest{Cursor: cursor, Limit: limit})
		if e != nil {
			return e
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
		return c.emit("history", v)
	}}
	cmd.Flags().StringVar(&cursor, "cursor", "", "opaque pagination cursor")
	cmd.Flags().IntVar(&limit, "limit", controlplane.DefaultPageSize, "events per page")
	cmd.Flags().StringVar(&actor, "actor", "", "only events signed by this actor in the current page")
	cmd.Flags().StringVar(&keyFingerprint, "key-fingerprint", "", "only events signed by this key fingerprint in the current page")
	return cmd
}
func (c *cli) searchCmd() *cobra.Command {
	var cursor string
	var limit int
	var keyFingerprint string
	cmd := &cobra.Command{Use: "search <query>", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		q := strings.ToLower(strings.Join(args, " "))
		page, e := c.svc.History(controlplane.PageRequest{Cursor: cursor, Limit: limit})
		if e != nil {
			return e
		}
		out := []controlplane.EventRecord{}
		for _, v := range page.Items {
			if keyFingerprint != "" && v.Event.ActorKeyFingerprint != keyFingerprint {
				continue
			}
			b, _ := json.Marshal(v)
			if strings.Contains(strings.ToLower(string(b)), q) {
				out = append(out, v)
			}
		}
		return c.emit("search", map[string]any{
			"current_events": out,
			"next_cursor":    page.NextCursor, "metadata": page.Metadata,
		})
	}}
	cmd.Flags().StringVar(&cursor, "cursor", "", "opaque pagination cursor")
	cmd.Flags().IntVar(&limit, "limit", controlplane.DefaultPageSize, "events scanned per page")
	cmd.Flags().StringVar(&keyFingerprint, "key-fingerprint", "", "only events signed by this key fingerprint in the current page")
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
func payloadStatus(c *cli, domain, sub string, f func(string) any) *cobra.Command {
	cmd := &cobra.Command{Use: sub, RunE: func(cmd *cobra.Command, args []string) error {
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
