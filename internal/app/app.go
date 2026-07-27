package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
	"github.com/DhanushSantosh/AgentComms/internal/claudeserve"
	"github.com/DhanushSantosh/AgentComms/internal/claudetail"
	"github.com/DhanushSantosh/AgentComms/internal/codexserve"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/daemon"
	"github.com/DhanushSantosh/AgentComms/internal/daemonclient"
	"github.com/DhanushSantosh/AgentComms/internal/failure"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/interactiveserve"
	"github.com/DhanushSantosh/AgentComms/internal/mcp"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/onboarding"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
	"github.com/DhanushSantosh/AgentComms/internal/runtimeinit"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/sessionbind"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	tuiterm "github.com/DhanushSantosh/AgentComms/internal/tui"
	runtimeworker "github.com/DhanushSantosh/AgentComms/internal/worker"
	"github.com/spf13/cobra"
)

var Version = buildinfo.Version

const APIVersion = "agent-comms/v1"

const directDeliveryTimeout = 15 * time.Second

type Envelope struct {
	APIVersion string     `json:"api_version"`
	OK         bool       `json:"ok"`
	Command    string     `json:"command"`
	Result     any        `json:"result,omitempty"`
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

type cli struct {
	out, err                             io.Writer
	json, nonInteractive, noColor, quiet bool
	project, profile, actor              string
	timeout                              time.Duration
	svc                                  *service.Service
	cmd                                  string
	actorResolution                      identity.ActorResolution
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
		if c.json {
			body := Envelope{APIVersion: APIVersion, OK: false, Command: c.cmd, Error: &ErrorBody{Code: errorCode(e), Message: e.Error()}}
			_ = json.NewEncoder(stderr).Encode(body)
		}
		return &ExitError{Code: exitCode(e), Kind: errorCode(e), Err: e}
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
			cmd.CommandPath() == "agent-comms runtime interactive-serve" {
			return nil
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
		c.actorResolution, e = identity.ResolveActor(identity.ActorResolutionRequest{
			ProjectID: cfg.ProjectID, ProjectOwner: cfg.Owner,
			ExplicitActor: c.actor, ExplicitProfile: c.profile,
			EnvironmentActor: environmentActor, HostLabel: os.Getenv("AGENT_COMMS_HOST_LABEL"),
			UserConfig: userConfig,
		})
		if e != nil {
			return e
		}
		c.actor = c.actorResolution.Actor
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
	if c.json {
		return json.NewEncoder(c.out).Encode(Envelope{APIVersion: APIVersion, OK: true, Command: command, Result: v, Warnings: warnings})
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
	for _, w := range warnings {
		if _, e = fmt.Fprintln(c.err, "warning:", w); e != nil {
			return e
		}
	}
	return nil
}

// notifyInteractiveTarget resolves an invocation's target agent to its
// registered runtime before attempting the best-effort terminal nudge. A
// same-ID fallback preserves the zero-registration interactive workflow.
// Multiple live sessions are not guessed between: waking an arbitrary
// runtime could make the wrong session race to claim the invocation.
func (c *cli) notifyInteractiveTarget(invocationID, targetAgentID, preferredRuntimeID string) []string {
	state, err := c.svc.State()
	if err != nil {
		return []string{fmt.Sprintf("invocation was recorded, but live runtime resolution failed: %v", err)}
	}
	candidates := interactiveRuntimeCandidates(state, targetAgentID)
	if preferredRuntimeID != "" {
		runtimeState, registered := state.AgentRuntimes[preferredRuntimeID]
		if preferredRuntimeID != targetAgentID &&
			(!registered || runtimeState.AgentID != targetAgentID ||
				runtimeState.Status == "DRAINING" || runtimeState.Status == "REVOKED") {
			return []string{fmt.Sprintf(
				"runtime %s is not an eligible runtime for agent %s",
				preferredRuntimeID, targetAgentID,
			)}
		}
		candidates = []string{preferredRuntimeID}
	}
	type liveRuntime struct {
		id   string
		busy bool
	}
	live := make([]liveRuntime, 0, len(candidates))
	for _, runtimeID := range candidates {
		ctx, cancel := context.WithTimeout(context.Background(), directDeliveryTimeout)
		alive, busy := interactiveserve.Probe(ctx, c.svc.Store.Root, runtimeID)
		cancel()
		if alive {
			live = append(live, liveRuntime{id: runtimeID, busy: busy})
		}
	}
	if len(live) == 0 {
		return nil
	}
	if len(live) > 1 {
		ids := make([]string, 0, len(live))
		for _, runtime := range live {
			ids = append(ids, runtime.id)
		}
		return []string{fmt.Sprintf(
			"invocation was recorded, but agent %s has multiple live runtimes (%s); use `invocation redeliver --id %s --runtime <runtime-id>`",
			targetAgentID, strings.Join(ids, ", "), invocationID,
		)}
	}
	target := live[0]
	if target.busy {
		return []string{fmt.Sprintf(
			"invocation was recorded, but runtime %s is busy; retry with `invocation redeliver --id %s` when it is idle",
			target.id, invocationID,
		)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), directDeliveryTimeout)
	defer cancel()
	if err = interactiveserve.NotifyInvocation(ctx, c.svc.Store.Root, target.id, targetAgentID, invocationID, c.actor); err != nil {
		return []string{fmt.Sprintf("could not deliver directly into %s's live session: %v", target.id, err)}
	}
	return nil
}

func interactiveRuntimeCandidates(state model.State, targetAgentID string) []string {
	candidateSet := map[string]struct{}{targetAgentID: {}}
	for runtimeID, agentRuntime := range state.AgentRuntimes {
		if agentRuntime.AgentID != targetAgentID ||
			agentRuntime.Status == "DRAINING" || agentRuntime.Status == "REVOKED" {
			continue
		}
		candidateSet[runtimeID] = struct{}{}
	}
	candidates := make([]string, 0, len(candidateSet))
	for runtimeID := range candidateSet {
		candidates = append(candidates, runtimeID)
	}
	sort.Strings(candidates)
	return candidates
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

func errorCode(e error) string {
	var lifecycleError *projectlifecycle.Error
	if errors.As(e, &lifecycleError) {
		return string(lifecycleError.Code)
	}
	return failure.Code(e)
}
func exitCode(e error) int {
	var lifecycleError *projectlifecycle.Error
	if errors.As(e, &lifecycleError) {
		switch lifecycleError.Code {
		case projectlifecycle.CodeUpgradeRequired, projectlifecycle.CodeProjectTooNew, projectlifecycle.CodeUpgradeUnsupported:
			return 11
		case projectlifecycle.CodeUpgradeFailed:
			return 12
		case projectlifecycle.CodeConflict:
			return 9
		}
	}
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
	if !allKnown {
		root := c.project
		if root == "" {
			var err error
			root, err = os.Getwd()
			if err != nil {
				return nil, err
			}
		}
		return []string{root}, nil
	}
	config, err := identity.LoadUserConfig()
	if err != nil {
		return nil, err
	}
	unique := map[string]struct{}{}
	for _, profile := range config.Profiles {
		if strings.TrimSpace(profile.ProjectRoot) == "" {
			continue
		}
		absolute, absoluteErr := filepath.Abs(profile.ProjectRoot)
		if absoluteErr != nil {
			return nil, absoluteErr
		}
		unique[filepath.Clean(absolute)] = struct{}{}
	}
	roots := make([]string, 0, len(unique))
	for root := range unique {
		roots = append(roots, root)
	}
	sort.Strings(roots)
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
		type finding struct {
			Severity string `json:"severity"`
			Code     string `json:"code"`
			Message  string `json:"message"`
			Guidance string `json:"guidance"`
		}
		findings := []finding{}
		add := func(severity, code, message, guidance string) {
			findings = append(findings, finding{severity, code, message, guidance})
		}
		if cfg.SchemaVersion != model.SchemaVersion {
			add("ERROR", "RUNTIME_SCHEMA_MISMATCH", fmt.Sprintf("binary expects schema %s but runtime is %s", model.SchemaVersion, cfg.SchemaVersion), "Use the Agent Comms version that created this project or initialize a new project.")
		}
		if cfg.ToolkitVersion == "" {
			add("WARNING", "RUNTIME_VERSION_UNKNOWN", "runtime does not record the toolkit version that created it", "Use the Agent Comms version that initialized this project.")
		} else if cfg.ToolkitVersion != Version {
			add("WARNING", "BINARY_RUNTIME_VERSION_MISMATCH", fmt.Sprintf("installed binary is %s but runtime was prepared by %s", Version, cfg.ToolkitVersion), "Install the intended release before normal work.")
		}
		if !c.svc.Store.ManagedBootstrapValid() {
			add("ERROR", "MANAGED_BOOTSTRAP_MISSING", "project root .agents does not match the configured authority mode", "Restore the bootstrap for this project before normal work.")
		}
		if !c.svc.Store.InstructionsPresent() {
			add("ERROR", "AGENT_INSTRUCTIONS_MISSING", ".agent-comms/AGENT_INSTRUCTIONS.md is missing or empty", "Restore the generated instructions with the matching Agent Comms release.")
		}
		if cfg.SchemaVersion == model.SchemaVersion {
			if st, x := c.svc.State(); x == nil {
				now := time.Now().UTC()
				for id, task := range st.Tasks {
					if !task.LeaseUntil.IsZero() && now.After(task.LeaseUntil) && task.Status != "COMPLETED" && task.Status != "CANCELLED" {
						add("WARNING", "STALE_LEASE", fmt.Sprintf("task %s lease expired at %s", id, task.LeaseUntil.Format(time.RFC3339)), "An orchestrator must review it; stale work is never reassigned automatically.")
					}
				}
				for id := range st.Agents {
					if strings.EqualFold(id, "builder") || strings.Contains(strings.ToLower(id), "test") || strings.Contains(strings.ToLower(id), "smoke") {
						add("WARNING", "TEST_LIKE_RUNTIME", "runtime contains test-like agent identity "+id, "Verify every identity explicitly before activation.")
						break
					}
				}
				for id := range st.Tasks {
					if strings.EqualFold(id, "task-001") || strings.Contains(strings.ToLower(id), "test") || strings.Contains(strings.ToLower(id), "smoke") {
						add("WARNING", "TEST_LIKE_RUNTIME", "runtime contains test-like task "+id, "Verify or remove synthetic state through governed commands.")
						break
					}
				}
				invocationTerminal := map[string]bool{"COMPLETED": true, "REJECTED": true, "EXPIRED": true, "DEAD_LETTER": true}
				taskTerminal := map[string]bool{"COMPLETED": true, "CANCELLED": true}
				for id, agent := range st.Agents {
					if agent.Status != "REVOKED" {
						continue
					}
					openInvocations := 0
					for _, invocation := range st.Invocations {
						if (invocation.RequestedBy == id || invocation.Target == id) && !invocationTerminal[invocation.Status] {
							openInvocations++
						}
					}
					openTasks := 0
					for _, task := range st.Tasks {
						if task.Owner == id && !taskTerminal[task.Status] {
							openTasks++
						}
					}
					if openInvocations > 0 || openTasks > 0 {
						add("WARNING", "REVOKED_AGENT_HAS_OPEN_WORK",
							fmt.Sprintf("revoked agent %s still has %d open invocation(s) and %d owned task(s)", id, openInvocations, openTasks),
							"Use `invocation cancel`/`task takeover`/`task handoff` to resolve this separately; revocation does not auto-cancel or auto-reassign work.")
					}
				}
			}
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
		failedDeliveries := map[string]model.Invocation{}
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
			if invocation.Status == "DEAD_LETTER" {
				failedDeliveries[id] = invocation
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
	cmd := &cobra.Command{Use: "history", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		v, e := c.svc.History(controlplane.PageRequest{Cursor: cursor, Limit: limit})
		if e != nil {
			return e
		}
		return c.emit("history", v)
	}}
	cmd.Flags().StringVar(&cursor, "cursor", "", "opaque pagination cursor")
	cmd.Flags().IntVar(&limit, "limit", controlplane.DefaultPageSize, "events per page")
	return cmd
}
func (c *cli) searchCmd() *cobra.Command {
	var cursor string
	var limit int
	cmd := &cobra.Command{Use: "search <query>", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		q := strings.ToLower(strings.Join(args, " "))
		page, e := c.svc.History(controlplane.PageRequest{Cursor: cursor, Limit: limit})
		if e != nil {
			return e
		}
		out := []controlplane.EventRecord{}
		for _, v := range page.Items {
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
	return cmd
}
func (c *cli) agentCmd() *cobra.Command {
	root := &cobra.Command{Use: "agent"}
	var display, ptype string
	reg := &cobra.Command{Use: "register", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id != c.actor {
			can, e := c.svc.CanSponsorRegistration(c.actor)
			if e != nil {
				return e
			}
			if !can {
				return fmt.Errorf("agent register: registering a different id requires an active orchestrator or human principal (actor: %s)", c.actor)
			}
		}
		v, e := c.svc.Register(id, display, model.PrincipalType(strings.ToUpper(ptype)))
		if e != nil {
			return e
		}
		return c.emit("agent.register", v)
	}}
	reg.Flags().String("id", "", "principal ID")
	_ = reg.MarkFlagRequired("id")
	reg.Flags().StringVar(&display, "display-name", "", "display name")
	reg.Flags().StringVar(&ptype, "principal-type", "AGENT", "HUMAN or AGENT")
	var role string
	var caps, scopes []string
	act := &cobra.Command{Use: "activate", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		v, e := c.svc.Execute(c.actor, "agent.activate", id, model.AgentActivated{Role: model.Role(strings.ToUpper(role)), Capabilities: caps, Scopes: scopes})
		if e != nil {
			return e
		}
		return c.emit("agent.activate", v)
	}}
	act.Flags().String("id", "", "principal ID")
	_ = act.MarkFlagRequired("id")
	act.Flags().StringVar(&role, "role", "AGENT", "role")
	act.Flags().StringSliceVar(&caps, "capability", nil, "capability (repeatable or comma-separated)")
	act.Flags().StringSliceVar(&scopes, "scope", nil, "scope (repeatable or comma-separated)")
	suspend := simpleStatus(c, "agent", "suspend")
	var revokeReason string
	revoke := payloadStatus(c, "agent", "revoke", func(string) any {
		return model.RuntimeStatusChanged{Reason: revokeReason}
	})
	revoke.Flags().StringVar(&revokeReason, "reason", "", "revocation reason")
	rotate := &cobra.Command{Use: "rotate-key", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		v, e := c.svc.RotateKey(c.actor)
		if e != nil {
			return e
		}
		return c.emit("agent.rotate-key", v)
	}}
	var newDisplayName string
	rename := &cobra.Command{Use: "rename", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		v, e := c.svc.Execute(c.actor, "agent.rename", id, model.AgentRenamed{DisplayName: newDisplayName})
		if e != nil {
			return e
		}
		return c.emit("agent.rename", v)
	}}
	rename.Flags().String("id", "", "principal ID")
	_ = rename.MarkFlagRequired("id")
	rename.Flags().StringVar(&newDisplayName, "display-name", "", "new display name")
	_ = rename.MarkFlagRequired("display-name")
	list := &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		return c.emit("agent.list", st.Agents)
	}}
	root.AddCommand(reg, act, suspend, rotate, rename, revoke, list)
	return root
}
func (c *cli) runtimeCmd() *cobra.Command {
	root := &cobra.Command{Use: "runtime", Short: "Manage agent runtime connectors and presence"}
	var agentID, connector, configReference string
	var maxConcurrent int
	var scopes, capabilities []string
	register := &cobra.Command{Use: "register", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		value, err := c.svc.Execute(c.actor, "runtime.register", id, model.RuntimeRegistered{
			AgentID: agentID, Connector: connector, ConfigReference: configReference,
			MaxConcurrent: maxConcurrent, Scopes: scopes, Capabilities: capabilities,
		})
		if err != nil {
			return err
		}
		c.captureRuntimeSession(id)
		return c.emit("runtime.register", value)
	}}
	register.Flags().String("id", "", "runtime ID")
	_ = register.MarkFlagRequired("id")
	register.Flags().StringVar(&agentID, "agent", "", "agent that owns the runtime")
	_ = register.MarkFlagRequired("agent")
	register.Flags().StringVar(&connector, "connector", "MANUAL", "MANUAL, MCP, LOCAL_PROCESS, WEBHOOK, or QUEUE")
	register.Flags().StringVar(&configReference, "config-reference", "", "non-secret local connector configuration reference")
	register.Flags().IntVar(&maxConcurrent, "max-concurrent", 1, "maximum concurrent invocations")
	register.Flags().StringSliceVar(&scopes, "scope", nil, "runtime scope")
	register.Flags().StringSliceVar(&capabilities, "capability", nil, "runtime capability")

	var health string
	var activeInvocations []string
	heartbeat := &cobra.Command{Use: "heartbeat", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		value, err := c.svc.Execute(c.actor, "runtime.heartbeat", id, model.RuntimeHeartbeat{
			Health: health, ActiveInvocations: activeInvocations,
		})
		if err != nil {
			return err
		}
		return c.emit("runtime.heartbeat", value)
	}}
	heartbeat.Flags().String("id", "", "runtime ID")
	_ = heartbeat.MarkFlagRequired("id")
	heartbeat.Flags().StringVar(&health, "health", "HEALTHY", "HEALTHY or DEGRADED")
	heartbeat.Flags().StringSliceVar(&activeInvocations, "active-invocation", nil, "active invocation ID")

	for _, operation := range []string{"drain", "resume", "revoke"} {
		operation := operation
		var reason string
		command := &cobra.Command{Use: operation, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetString("id")
			value, err := c.svc.Execute(c.actor, "runtime."+operation, id, model.RuntimeStatusChanged{Reason: reason})
			if err != nil {
				return err
			}
			return c.emit("runtime."+operation, value)
		}}
		command.Flags().String("id", "", "runtime ID")
		_ = command.MarkFlagRequired("id")
		command.Flags().StringVar(&reason, "reason", "", "status-change reason")
		root.AddCommand(command)
	}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		state, err := c.svc.State()
		if err != nil {
			return err
		}
		return c.emit("runtime.list", state.AgentRuntimes)
	}}
	var workerAdapter, workerExecutable, workerModel, workerSessionID, permissionMode, sandbox string
	var codexAddDirs []string
	var executionTimeout, listenWait time.Duration
	var claudeBudget float64
	var once, allowAgentComms, codexIgnoreUserConfig bool
	workerCommand := &cobra.Command{
		Use:   "worker",
		Short: "Run a governed autonomous Claude or Codex invocation worker",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeID, _ := cmd.Flags().GetString("id")
			if workerSessionID == "" {
				if binding, ok, lookupErr := sessionbind.Load(c.svc.Store.Root, runtimeID); lookupErr == nil && ok && binding.Adapter == workerAdapter {
					workerSessionID = binding.SessionID
					if !c.quiet {
						_, _ = fmt.Fprintf(c.err, "using captured %s session %s for runtime %s\n", binding.Adapter, binding.SessionID, runtimeID)
					}
				}
			}
			executable := workerExecutable
			if executable == "" && runtimeworker.RequiresExecutable(workerAdapter) {
				var lookupErr error
				executable, lookupErr = exec.LookPath(workerAdapter)
				if lookupErr != nil {
					return fmt.Errorf("locate %s executable: %w", workerAdapter, lookupErr)
				}
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			agentCommsPath := ""
			if allowAgentComms {
				var pathErr error
				agentCommsPath, pathErr = os.Executable()
				if pathErr != nil {
					return fmt.Errorf("locate Agent Comms executable: %w", pathErr)
				}
				agentCommsPath, pathErr = filepath.EvalSymlinks(agentCommsPath)
				if pathErr != nil {
					return fmt.Errorf("resolve Agent Comms executable: %w", pathErr)
				}
			}
			instance, err := runtimeworker.New(runtimeworker.Config{
				Service: c.svc, Actor: c.actor, RuntimeID: runtimeID, SessionID: workerSessionID,
				Adapter: workerAdapter, Executable: executable, WorkDir: c.svc.Store.Root,
				Model: workerModel, PermissionMode: permissionMode, Sandbox: sandbox,
				CodexAddDirs: codexAddDirs, CodexIgnoreUserConfig: codexIgnoreUserConfig,
				ExecutionTimeout: executionTimeout, ListenWait: listenWait,
				ClaudeBudgetUSD: claudeBudget, AgentCommsPath: agentCommsPath, Once: once,
				Status: func(status string) {
					if !c.quiet {
						_, _ = fmt.Fprintln(c.err, status)
					}
				},
			})
			if err != nil {
				return err
			}
			return instance.Run(ctx)
		},
	}
	workerCommand.Flags().String("id", "", "registered runtime ID")
	_ = workerCommand.MarkFlagRequired("id")
	workerCommand.Flags().StringVar(&workerAdapter, "adapter", "claude", "claude, codex, opencode, claude-live, codex-live, opencode-live, claude-acp, opencode-acp, or codex-acp")
	workerCommand.Flags().StringVar(&workerExecutable, "executable", "", "absolute agent executable path")
	workerCommand.Flags().StringVar(&workerModel, "model", "", "agent model override")
	workerCommand.Flags().StringVar(&workerSessionID, "session-id", "", "existing Claude or Codex conversation UUID to resume")
	workerCommand.Flags().StringVar(&permissionMode, "claude-permission-mode", "acceptEdits", "Claude permission mode without bypass")
	workerCommand.Flags().StringVar(&sandbox, "codex-sandbox", "workspace-write", "Codex read-only or workspace-write sandbox")
	workerCommand.Flags().StringSliceVar(&codexAddDirs, "codex-add-dir", nil, "additional absolute writable directory for Codex (repeatable)")
	workerCommand.Flags().BoolVar(&codexIgnoreUserConfig, "codex-ignore-user-config", false, "isolate autonomous runs from user MCP and tool configuration")
	workerCommand.Flags().DurationVar(&executionTimeout, "execution-timeout", 30*time.Minute, "per-invocation execution timeout")
	workerCommand.Flags().DurationVar(&listenWait, "listen-wait", controlplane.MaxInvocationListen, "bounded invocation listen duration")
	workerCommand.Flags().Float64Var(&claudeBudget, "claude-max-budget-usd", 1, "Claude spend ceiling (per invocation for claude; per process for claude-live)")
	workerCommand.Flags().BoolVar(&allowAgentComms, "claude-allow-agent-comms", false, "allow only this Agent Comms executable as an unattended Claude Bash command")
	workerCommand.Flags().BoolVar(&once, "once", false, "process at most one invocation and exit")

	var bindSessionID string
	bindSession := &cobra.Command{
		Use:   "bind-session",
		Short: "Capture the current Claude conversation and bind it to a registered runtime",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, adapter := sessionbind.Capture()
			if sessionID == "" {
				return errors.New("no supported provider session was found in the current environment (set CLAUDE_CODE_SESSION_ID, or run this from inside a live Claude Code session)")
			}
			if err := sessionbind.Save(c.svc.Store.Root, bindSessionID, sessionID, adapter); err != nil {
				return err
			}
			return c.emit("runtime.bind-session", map[string]any{
				"runtime_id": bindSessionID, "adapter": adapter, "session_id": sessionID,
			})
		},
	}
	bindSession.Flags().StringVar(&bindSessionID, "id", "", "runtime ID")
	_ = bindSession.MarkFlagRequired("id")

	var sessionRuntimeID string
	sessionShow := &cobra.Command{
		Use:   "session",
		Short: "Show the locally captured session binding for a runtime",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			binding, ok, err := sessionbind.Load(c.svc.Store.Root, sessionRuntimeID)
			if err != nil {
				return err
			}
			if !ok {
				return c.emit("runtime.session", map[string]any{"runtime_id": sessionRuntimeID, "bound": false})
			}
			return c.emit("runtime.session", map[string]any{
				"runtime_id": sessionRuntimeID, "bound": true, "adapter": binding.Adapter,
				"session_id": binding.SessionID, "captured_at": binding.CapturedAt,
			})
		},
	}
	sessionShow.Flags().StringVar(&sessionRuntimeID, "id", "", "runtime ID")
	_ = sessionShow.MarkFlagRequired("id")

	var interactiveServeID string
	var interactiveClaudeAllowAgentComms bool
	interactiveServe := &cobra.Command{
		Use:   "interactive-serve --id <runtimeID> -- <command> [args...]",
		Short: "Own a real pty running <command>, dialable by other runtimes for direct invocation delivery",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().ArgsLenAtDash() == -1 {
				return errors.New(`interactive-serve requires "--" before the wrapped command, e.g. "agent-comms runtime interactive-serve --id codex-runner -- codex"`)
			}
			if interactiveClaudeAllowAgentComms {
				var err error
				args, err = withClaudeAllowAgentComms(args, os.Executable)
				if err != nil {
					return err
				}
			}
			root := c.project
			if root == "" {
				var e error
				root, e = os.Getwd()
				if e != nil {
					return e
				}
			}
			code, err := interactiveserve.Serve(cmd.Context(), interactiveserve.ServeOptions{
				ProjectRoot: root, RuntimeID: interactiveServeID, Command: args,
			})
			if err != nil {
				return err
			}
			os.Exit(code)
			return nil
		},
	}
	interactiveServe.Flags().StringVar(&interactiveServeID, "id", "", "runtime ID")
	_ = interactiveServe.MarkFlagRequired("id")
	interactiveServe.Flags().BoolVar(&interactiveClaudeAllowAgentComms, "claude-allow-agent-comms", false, "wrapped command must be claude; scopes unattended Bash permission to this Agent Comms executable only")

	var interactiveShowID string
	interactiveShow := &cobra.Command{
		Use:   "interactive-session",
		Short: "Show whether a runtime has a live interactive-serve session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			alive := interactiveserve.Alive(cmd.Context(), c.svc.Store.Root, interactiveShowID)
			return c.emit("runtime.interactive-session", map[string]any{"runtime_id": interactiveShowID, "alive": alive})
		},
	}
	interactiveShow.Flags().StringVar(&interactiveShowID, "id", "", "runtime ID")
	_ = interactiveShow.MarkFlagRequired("id")

	root.AddCommand(register, heartbeat, list, workerCommand, bindSession, sessionShow,
		interactiveServe, interactiveShow)
	return root
}

// withClaudeAllowAgentComms validates that args wraps claude (by basename)
// and, if so, appends --allowedTools rules scoped to agent-comms so a claude
// runtime under interactive-serve can drive `agent-comms invocation *`
// unattended without gaining Bash access to anything else.
//
// Two rules are appended, not one: the resolved absolute path (the same
// scoping runtimeCmd's `worker --claude-allow-agent-comms` flag already
// uses), and the bare basename. Confirmed live this only works with both —
// the notification `interactiveserve.NotifyInvocation` delivers (and Claude's
// own follow-up `invocation claim/start/complete` calls) all invoke the bare
// "agent-comms" name, relying on PATH, never the resolved absolute path, so
// an absolute-path-only rule silently never matches anything and every call
// still prompts for approval — this was built once, assumed to work by
// analogy with the worker-mode flag, and only caught by an actual live
// 3-way test. The basename rule is what makes approval friction actually go
// away; the absolute-path rule is kept alongside it as defense in depth for
// the case where something does invoke the resolved path directly.
// executablePath is injected (rather than calling os.Executable directly)
// so tests can exercise this without depending on the actual test binary's
// path.
func withClaudeAllowAgentComms(args []string, executablePath func() (string, error)) ([]string, error) {
	if len(args) == 0 || filepath.Base(args[0]) != "claude" {
		return nil, errors.New("--claude-allow-agent-comms only applies when the wrapped command is claude")
	}
	agentCommsPath, err := executablePath()
	if err != nil {
		return nil, fmt.Errorf("locate Agent Comms executable: %w", err)
	}
	agentCommsPath, err = filepath.EvalSymlinks(agentCommsPath)
	if err != nil {
		return nil, fmt.Errorf("resolve Agent Comms executable: %w", err)
	}
	out := append([]string{}, args...)
	out = append(out, "--allowedTools", "Bash("+agentCommsPath+" *)")
	out = append(out, "--allowedTools", "Bash("+filepath.Base(agentCommsPath)+" *)")
	return out, nil
}

func (c *cli) invocationCmd() *cobra.Command {
	root := &cobra.Command{Use: "invocation", Short: "Request and process agent invocations"}
	var target, messageID, taskID, instruction, expectedResult, priority string
	var invocationScopes []string
	var expiresIn time.Duration
	request := &cobra.Command{Use: "request", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			id = fmt.Sprintf("inv-%d", time.Now().UnixNano())
		}
		var deadline *time.Time
		if expiresIn > 0 {
			value := time.Now().UTC().Add(expiresIn)
			deadline = &value
		}
		event, err := c.svc.Execute(c.actor, "invocation.request", id, model.InvocationRequested{
			Target: target, MessageID: messageID, TaskID: taskID, Instruction: instruction,
			ExpectedResult: expectedResult, Scopes: invocationScopes, Priority: priority, Deadline: deadline,
		})
		if err != nil {
			return err
		}
		return c.emit("invocation.request", event, c.notifyInteractiveTarget(id, target, "")...)
	}}
	request.Flags().String("id", "", "invocation ID (auto-generated if omitted)")
	request.Flags().StringVar(&target, "to", "", "target agent")
	_ = request.MarkFlagRequired("to")
	request.Flags().StringVar(&messageID, "message", "", "related message ID")
	request.Flags().StringVar(&taskID, "task", "", "related task ID")
	request.Flags().StringVar(&instruction, "instruction", "", "bounded instruction for the target agent")
	_ = request.MarkFlagRequired("instruction")
	request.Flags().StringVar(&expectedResult, "expected-result", "", "expected result")
	request.Flags().StringSliceVar(&invocationScopes, "scope", nil, "scope required by the invocation")
	request.Flags().StringVar(&priority, "priority", "NORMAL", "LOW, NORMAL, HIGH, or URGENT")
	request.Flags().DurationVar(&expiresIn, "expires-in", 0, "deadline relative to now")

	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		state, err := c.svc.State()
		if err != nil {
			return err
		}
		status, _ := cmd.Flags().GetString("status")
		targetFilter, _ := cmd.Flags().GetString("to")
		result := map[string]model.Invocation{}
		for id, invocation := range state.Invocations {
			if status != "" && invocation.Status != strings.ToUpper(status) {
				continue
			}
			if targetFilter != "" && invocation.Target != targetFilter {
				continue
			}
			result[id] = invocation
		}
		return c.emit("invocation.list", result)
	}}
	list.Flags().String("status", "", "filter by status")
	list.Flags().String("to", "", "filter by target agent")

	next := &cobra.Command{Use: "next", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		runtimeID, _ := cmd.Flags().GetString("runtime")
		invocation, found, err := c.svc.NextInvocation(c.actor, runtimeID)
		if err != nil {
			return err
		}
		return c.emit("invocation.next", map[string]any{"found": found, "invocation": invocation})
	}}
	next.Flags().String("runtime", "", "runtime ID used for capacity filtering")

	var redeliveryRuntimeID string
	redeliver := &cobra.Command{Use: "redeliver", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		state, err := c.svc.State()
		if err != nil {
			return err
		}
		invocation, ok := state.Invocations[id]
		if !ok {
			return fmt.Errorf("invocation %s not found", id)
		}
		if invocation.Status != "PENDING" {
			return fmt.Errorf("invocation %s is %s, not PENDING", id, invocation.Status)
		}
		return c.emit("invocation.redeliver", map[string]any{"id": id, "target": invocation.Target},
			c.notifyInteractiveTarget(id, invocation.Target, redeliveryRuntimeID)...)
	}}
	redeliver.Flags().String("id", "", "invocation ID to re-attempt direct delivery for")
	_ = redeliver.MarkFlagRequired("id")
	redeliver.Flags().StringVar(&redeliveryRuntimeID, "runtime", "", "specific eligible runtime when the target agent has multiple live runtimes")

	var runtimeID string
	var listenDuration time.Duration
	var autoClaim bool
	listen := &cobra.Command{Use: "listen", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		invocation, found, err := c.svc.ListenInvocation(c.actor, runtimeID, listenDuration)
		if err != nil {
			return err
		}
		result := map[string]any{"found": found, "invocation": invocation, "claimed": false}
		if found && autoClaim {
			event, claimErr := c.svc.Execute(c.actor, "invocation.claim", invocation.ID,
				model.InvocationClaimed{RuntimeID: runtimeID})
			if claimErr != nil {
				return claimErr
			}
			result["claimed"] = true
			result["claim_event"] = event
		}
		return c.emit("invocation.listen", result)
	}}
	listen.Flags().StringVar(&runtimeID, "runtime", "", "connected runtime ID")
	_ = listen.MarkFlagRequired("runtime")
	listen.Flags().DurationVar(&listenDuration, "wait", controlplane.MaxInvocationListen, "bounded listen duration")
	listen.Flags().BoolVar(&autoClaim, "claim", true, "atomically claim a delivered invocation")

	claim := payloadStatus(c, "invocation", "claim", func(string) any {
		return model.InvocationClaimed{RuntimeID: runtimeID}
	})
	claim.Flags().StringVar(&runtimeID, "runtime", "", "claiming runtime ID")
	_ = claim.MarkFlagRequired("runtime")
	var summary string
	start := payloadStatus(c, "invocation", "start", func(string) any { return model.InvocationProgress{Summary: summary} })
	start.Flags().StringVar(&summary, "summary", "", "progress summary")
	var waitReason string
	var retryIn time.Duration
	waitCommand := payloadStatus(c, "invocation", "wait", func(string) any {
		var nextAttempt *time.Time
		if retryIn > 0 {
			value := time.Now().UTC().Add(retryIn)
			nextAttempt = &value
		}
		return model.InvocationWaiting{Reason: waitReason, NextAttemptAt: nextAttempt}
	})
	waitCommand.Flags().StringVar(&waitReason, "reason", "", "waiting reason")
	_ = waitCommand.MarkFlagRequired("reason")
	waitCommand.Flags().DurationVar(&retryIn, "retry-in", 0, "next attempt relative to now")
	resume := payloadStatus(c, "invocation", "resume", func(string) any { return model.InvocationProgress{Summary: summary} })
	resume.Flags().StringVar(&summary, "summary", "", "progress summary")
	var resultMessage string
	complete := payloadStatus(c, "invocation", "complete", func(string) any {
		return model.InvocationCompleted{ResultMessageID: resultMessage, Summary: summary}
	})
	complete.Flags().StringVar(&resultMessage, "result-message", "", "result message ID")
	complete.Flags().StringVar(&summary, "summary", "", "completion summary")
	_ = complete.MarkFlagRequired("summary")
	var reason string
	reject := payloadStatus(c, "invocation", "reject", func(string) any { return model.InvocationRejected{Reason: reason} })
	reject.Flags().StringVar(&reason, "reason", "", "rejection reason")
	_ = reject.MarkFlagRequired("reason")
	expire := payloadStatus(c, "invocation", "expire", func(string) any { return model.InvocationRejected{Reason: reason} })
	expire.Flags().StringVar(&reason, "reason", "", "expiry reason")
	_ = expire.MarkFlagRequired("reason")
	cancelInvocation := payloadStatus(c, "invocation", "cancel", func(string) any { return model.InvocationRejected{Reason: reason} })
	cancelInvocation.Flags().StringVar(&reason, "reason", "", "cancellation reason")
	_ = cancelInvocation.MarkFlagRequired("reason")

	policy := c.invocationPolicyCmd()
	root.AddCommand(request, list, next, redeliver, listen, claim, start, waitCommand, resume, complete, reject, expire, cancelInvocation, policy)
	return root
}

func (c *cli) invocationPolicyCmd() *cobra.Command {
	root := &cobra.Command{Use: "policy", Short: "Manage per-agent invocation policy"}
	var mode string
	var trustedActors, allowedScopes []string
	var requireHuman bool
	set := &cobra.Command{Use: "set", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		agentID, _ := cmd.Flags().GetString("agent")
		event, err := c.svc.Execute(c.actor, "invocation.policy.update", agentID, model.InvocationPolicyUpdated{
			Mode: mode, TrustedActors: trustedActors, AllowedScopes: allowedScopes,
			RequireHumanForSensitive: requireHuman,
		})
		if err != nil {
			return err
		}
		return c.emit("invocation.policy.update", event)
	}}
	set.Flags().String("agent", "", "target agent")
	_ = set.MarkFlagRequired("agent")
	set.Flags().StringVar(&mode, "mode", "MANUAL", "MANUAL, TRUSTED, AUTOMATIC, or DISABLED")
	set.Flags().StringSliceVar(&trustedActors, "trusted-actor", nil, "actor allowed by TRUSTED mode")
	set.Flags().StringSliceVar(&allowedScopes, "scope", nil, "allowed invocation scope")
	set.Flags().BoolVar(&requireHuman, "require-human-for-sensitive", true, "require human approval for sensitive work")
	show := &cobra.Command{Use: "show", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		agentID, _ := cmd.Flags().GetString("agent")
		state, err := c.svc.State()
		if err != nil {
			return err
		}
		return c.emit("invocation.policy.show", state.InvocationPolicies[agentID])
	}}
	show.Flags().String("agent", "", "target agent")
	_ = show.MarkFlagRequired("agent")
	root.AddCommand(set, show)
	return root
}

func (c *cli) sessionCmd() *cobra.Command {
	root := &cobra.Command{Use: "session"}
	for _, sub := range []string{"start", "end"} {
		sub := sub
		cmd := &cobra.Command{Use: sub, RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetString("id")
			v, e := c.svc.Execute(c.actor, "session."+sub, id, model.SessionPayload{AgentID: c.actor, PID: os.Getpid()})
			if e != nil {
				return e
			}
			return c.emit("session."+sub, v)
		}}
		cmd.Flags().String("id", "", "session ID")
		_ = cmd.MarkFlagRequired("id")
		root.AddCommand(cmd)
	}
	heartbeat := &cobra.Command{Use: "heartbeat", RunE: func(cmd *cobra.Command, args []string) error {
		return c.emit("session.heartbeat", map[string]any{"actor": c.actor, "at": time.Now().UTC(), "durable": false})
	}}
	root.AddCommand(heartbeat)
	return root
}
func (c *cli) taskCmd() *cobra.Command {
	root := &cobra.Command{Use: "task"}
	var title, summary, repo, branch, worktree, external, risk string
	var resources []string
	create := &cobra.Command{Use: "create", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		v, e := c.svc.Execute(c.actor, "task.create", id, model.TaskCreated{Title: title, Summary: summary, Repository: repo, Branch: branch, Worktree: worktree, Resources: resources, ExternalRef: external, Risk: risk})
		if e != nil {
			return e
		}
		return c.emit("task.create", v)
	}}
	create.Flags().String("id", "", "task ID")
	_ = create.MarkFlagRequired("id")
	create.Flags().StringVar(&title, "title", "", "title")
	create.Flags().StringVar(&summary, "summary", "", "summary")
	create.Flags().StringVar(&repo, "repository", "local", "repository")
	create.Flags().StringVar(&branch, "branch", "", "branch")
	create.Flags().StringVar(&worktree, "worktree", "", "worktree path")
	create.Flags().StringSliceVar(&resources, "resource", nil, "write resource")
	create.Flags().StringVar(&external, "external-ref", "", "external reference")
	create.Flags().StringVar(&risk, "risk", "ROUTINE", "risk tier")
	var to string
	var offerTTL time.Duration
	offer := &cobra.Command{Use: "offer", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		v, e := c.svc.Execute(c.actor, "task.offer", id, model.TaskOffered{To: to, ExpiresAt: time.Now().UTC().Add(offerTTL)})
		if e != nil {
			return e
		}
		return c.emit("task.offer", v)
	}}
	offer.Flags().String("id", "", "task ID")
	_ = offer.MarkFlagRequired("id")
	offer.Flags().StringVar(&to, "to", "", "principal")
	offer.Flags().DurationVar(&offerTTL, "expires-in", time.Hour, "offer validity")
	var leaseDuration time.Duration
	var claimRepo, claimWorktree string
	claim := &cobra.Command{Use: "claim", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		lease := time.Now().UTC().Add(leaseDuration)
		v, e := c.svc.Execute(c.actor, "task.claim", id, model.TaskClaimed{LeaseUntil: lease, Worktree: claimWorktree})
		if e != nil {
			return e
		}
		return c.emit("task.claim", v)
	}}
	claim.Flags().String("id", "", "task ID")
	_ = claim.MarkFlagRequired("id")
	claim.Flags().DurationVar(&leaseDuration, "duration", 4*time.Hour, "lease duration")
	claim.Flags().StringVar(&claimRepo, "repo", "", "repository path (acquires working-directory lock)")
	claim.Flags().StringVar(&claimWorktree, "worktree", "", "worktree path (acquires working-directory lock)")
	start := simpleStatus(c, "task", "start")
	var progress string
	renew := payloadStatus(c, "task", "renew", func(string) any { return model.TaskRenewed{Progress: progress} })
	renew.Flags().StringVar(&progress, "progress", "", "progress summary")
	block := statusWithSummary(c, "task", "block")
	review := statusWithSummary(c, "task", "review")
	complete := statusWithSummary(c, "task", "complete")
	cancel := statusWithSummary(c, "task", "cancel")
	var handTo, handSummary string
	handoff := &cobra.Command{Use: "handoff", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		accept, _ := cmd.Flags().GetBool("accept")
		typ := "task.handoff"
		var p any = model.TaskHandoff{To: handTo, Summary: handSummary}
		if accept {
			typ = "task.handoff.accept"
			p = model.TaskStatus{Summary: handSummary}
		}
		v, e := c.svc.Execute(c.actor, typ, id, p)
		if e != nil {
			return e
		}
		return c.emit(typ, v)
	}}
	handoff.Flags().String("id", "", "task ID")
	_ = handoff.MarkFlagRequired("id")
	handoff.Flags().StringVar(&handTo, "to", "", "handoff target")
	handoff.Flags().StringVar(&handSummary, "summary", "", "handoff summary")
	handoff.Flags().Bool("accept", false, "accept pending handoff")
	takeover := statusWithSummary(c, "task", "takeover")
	list := &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		return c.emit("task.list", st.Tasks)
	}}
	root.AddCommand(create, offer, claim, start, renew, block, review, complete, cancel, handoff, takeover, list)
	return root
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
func (c *cli) messageCmd() *cobra.Command {
	root := &cobra.Command{Use: "message"}
	var kind, subject, body, taskID, bodyFile string
	var to []string
	post := &cobra.Command{Use: "post", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			id = fmt.Sprintf("msg-%d", time.Now().UnixNano())
		}
		if bodyFile != "" {
			b, e := os.ReadFile(bodyFile)
			if e != nil {
				return e
			}
			body = string(b)
		}
		v, e := c.svc.Execute(c.actor, "message.post", id, model.MessagePosted{Kind: strings.ToUpper(kind), To: to, Subject: subject, Body: body, TaskID: taskID})
		if e != nil {
			return e
		}
		return c.emit("message.post", v)
	}}
	post.Flags().String("id", "", "message ID (auto-generated if omitted)")
	post.Flags().StringVar(&kind, "kind", "FYI", "message kind (FYI, ACTION, CONTRACT, BLOCKER, DECISION)")
	post.Flags().StringSliceVar(&to, "to", nil, "recipient")
	post.Flags().StringVar(&subject, "subject", "", "subject")
	post.Flags().StringVar(&body, "body", "", "body")
	post.Flags().StringVar(&bodyFile, "body-file", "", "read body from file (bypasses CLI arg limits)")
	post.Flags().StringVar(&taskID, "task", "", "related task")
	for _, sub := range []string{"ack", "reject", "complete", "resolve"} {
		postCmd := payloadStatus(c, "message", sub, func(string) any { return model.MessageResponse{} })
		root.AddCommand(postCmd)
	}
	inbox := &cobra.Command{Use: "inbox", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		unread, _ := cmd.Flags().GetBool("unread")
		from, _ := cmd.Flags().GetString("from")
		limit, _ := cmd.Flags().GetInt("limit")
		out := map[string]model.Message{}
		for id, m := range st.Messages {
			if unread && (m.Status != "OPEN" && m.Status != "DELIVERED") {
				continue
			}
			if from != "" && m.From != from {
				continue
			}
			for _, to := range m.To {
				if to == c.actor {
					out[id] = m
					break
				}
			}
		}
		if limit > 0 && len(out) > limit {
			trimmed := map[string]model.Message{}
			n := 0
			for id, m := range out {
				if n >= limit {
					break
				}
				trimmed[id] = m
				n++
			}
			out = trimmed
		}
		return c.emit("message.inbox", out)
	}}
	inbox.Flags().Bool("unread", false, "show only unread messages")
	inbox.Flags().String("from", "", "filter by sender")
	inbox.Flags().Int("limit", 0, "max results (0 = unlimited)")
	root.AddCommand(post, inbox)
	return root
}
func (c *cli) decisionCmd() *cobra.Command {
	root := &cobra.Command{Use: "decision"}
	for _, sub := range []string{"create", "supersede"} {
		sub := sub
		var title, statement, supersedes string
		var to []string
		cmd := &cobra.Command{Use: sub, RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetString("id")
			v, e := c.svc.Execute(c.actor, "decision."+sub, id, model.DecisionPayload{Title: title, Statement: statement, Supersedes: supersedes, To: to})
			if e != nil {
				return e
			}
			return c.emit("decision."+sub, v)
		}}
		cmd.Flags().String("id", "", "decision ID")
		_ = cmd.MarkFlagRequired("id")
		cmd.Flags().StringVar(&title, "title", "", "title")
		cmd.Flags().StringVar(&statement, "statement", "", "statement")
		cmd.Flags().StringVar(&supersedes, "supersedes", "", "prior decision")
		cmd.Flags().StringSliceVar(&to, "to", nil, "acknowledging principal")
		root.AddCommand(cmd)
	}
	return root
}
func (c *cli) approvalCmd() *cobra.Command {
	root := &cobra.Command{Use: "approval"}
	var tier, action, reason string
	var affected []string
	request := &cobra.Command{Use: "request", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		v, e := c.svc.Execute(c.actor, "approval.request", id, model.ApprovalRequested{Tier: strings.ToUpper(tier), Action: action, Reason: reason, Affected: affected})
		if e != nil {
			return e
		}
		return c.emit("approval.request", v)
	}}
	request.Flags().String("id", "", "approval ID")
	_ = request.MarkFlagRequired("id")
	request.Flags().StringVar(&tier, "tier", "ORCHESTRATOR", "ORCHESTRATOR or HUMAN")
	request.Flags().StringVar(&action, "action", "", "proposed action")
	request.Flags().StringVar(&reason, "reason", "", "reason")
	request.Flags().StringSliceVar(&affected, "affected", nil, "affected principal")
	approve := payloadStatus(c, "approval", "approve", func(string) any { return model.ApprovalResponse{} })
	reject := payloadStatus(c, "approval", "reject", func(string) any { return model.ApprovalResponse{} })
	list := &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		return c.emit("approval.list", st.Approvals)
	}}
	root.AddCommand(request, approve, reject, list)
	return root
}
func (c *cli) artifactCmd() *cobra.Command {
	root := &cobra.Command{Use: "artifact"}
	var path, hash string
	add := &cobra.Command{Use: "add", RunE: func(cmd *cobra.Command, args []string) error {
		v, e := c.svc.AddArtifact(c.actor, path)
		if e != nil {
			return e
		}
		return c.emit("artifact.add", v)
	}}
	add.Flags().StringVar(&path, "path", "", "artifact path")
	_ = add.MarkFlagRequired("path")
	show := &cobra.Command{Use: "show", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		a, ok := st.Artifacts[hash]
		if !ok {
			return errors.New("artifact not found")
		}
		return c.emit("artifact.show", a)
	}}
	show.Flags().StringVar(&hash, "sha256", "", "artifact digest")
	verify := &cobra.Command{Use: "verify", RunE: func(cmd *cobra.Command, args []string) error {
		p := filepath.Join(c.svc.Store.Root, store.Runtime, "artifacts", "sha256", hash)
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != hash {
			return errors.New("artifact hash mismatch")
		}
		return c.emit("artifact.verify", map[string]any{"verified": true, "sha256": hash, "size": len(b)})
	}}
	verify.Flags().StringVar(&hash, "sha256", "", "artifact digest")
	root.AddCommand(add, show, verify)
	return root
}
func (c *cli) documentCmd() *cobra.Command {
	root := &cobra.Command{Use: "document"}
	var title, body, docReplacement, bodyFile string
	var tags []string
	create := &cobra.Command{Use: "create", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if bodyFile != "" {
			b, e := os.ReadFile(bodyFile)
			if e != nil {
				return e
			}
			body = string(b)
		}
		if body == "" {
			return errors.New("body is required (use --body or --body-file)")
		}
		v, e := c.svc.Execute(c.actor, "document.create", id, model.DocumentPayload{Title: title, Body: body, Tags: tags})
		if e != nil {
			return e
		}
		return c.emit("document.create", v)
	}}
	create.Flags().String("id", "", "document ID")
	_ = create.MarkFlagRequired("id")
	create.Flags().StringVar(&title, "title", "", "title")
	_ = create.MarkFlagRequired("title")
	create.Flags().StringVar(&body, "body", "", "body")
	create.Flags().StringVar(&bodyFile, "body-file", "", "read body from file (bypasses CLI arg limits)")
	create.Flags().StringSliceVar(&tags, "tag", nil, "tag (repeatable)")
	update := &cobra.Command{Use: "update", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if bodyFile != "" {
			b, e := os.ReadFile(bodyFile)
			if e != nil {
				return e
			}
			body = string(b)
		}
		if body == "" {
			return errors.New("body is required (use --body or --body-file)")
		}
		v, e := c.svc.Execute(c.actor, "document.update", id, model.DocumentPayload{Title: title, Body: body, Tags: tags})
		if e != nil {
			return e
		}
		return c.emit("document.update", v)
	}}
	update.Flags().String("id", "", "document ID")
	_ = update.MarkFlagRequired("id")
	update.Flags().StringVar(&title, "title", "", "title")
	update.Flags().StringVar(&body, "body", "", "body")
	update.Flags().StringVar(&bodyFile, "body-file", "", "read body from file (bypasses CLI arg limits)")
	update.Flags().StringSliceVar(&tags, "tag", nil, "tag (repeatable)")
	supersede := &cobra.Command{Use: "supersede", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		docReplacement, _ = cmd.Flags().GetString("replacement")
		v, e := c.svc.Execute(c.actor, "document.supersede", id, model.DocumentPayload{ReplacementID: docReplacement})
		if e != nil {
			return e
		}
		return c.emit("document.supersede", v)
	}}
	supersede.Flags().String("id", "", "document ID")
	_ = supersede.MarkFlagRequired("id")
	supersede.Flags().StringVar(&docReplacement, "replacement", "", "replacement document ID")
	_ = supersede.MarkFlagRequired("replacement")
	list := &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		return c.emit("document.list", st.Documents)
	}}
	show := &cobra.Command{Use: "show", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" && len(args) > 0 {
			id = args[0]
		}
		if id == "" {
			return errors.New("document ID required (use --id or positional argument)")
		}
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		d, ok := st.Documents[id]
		if !ok {
			return fmt.Errorf("document %q not found", id)
		}
		return c.emit("document.show", d)
	}}
	show.Flags().String("id", "", "document ID")
	root.AddCommand(create, update, supersede, list, show)
	return root
}
func (c *cli) envCmd() *cobra.Command {
	root := &cobra.Command{Use: "env"}
	var key, value string
	set := &cobra.Command{Use: "set", RunE: func(cmd *cobra.Command, args []string) error {
		if key == "" && len(args) > 0 {
			key = args[0]
		}
		if value == "" && len(args) > 1 {
			value = args[1]
		}
		if key == "" {
			return errors.New("key required (use --key or positional argument)")
		}
		v, e := c.svc.Execute(c.actor, "env.set", "", model.EnvSetPayload{Key: key, Value: value})
		if e != nil {
			return e
		}
		return c.emit("env.set", v)
	}}
	set.Flags().StringVar(&key, "key", "", "key")
	set.Flags().StringVar(&value, "value", "", "value")
	get := &cobra.Command{Use: "get", RunE: func(cmd *cobra.Command, args []string) error {
		if key == "" && len(args) > 0 {
			key = args[0]
		}
		if key == "" {
			return errors.New("key required (use --key or positional argument)")
		}
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		entry, ok := st.Env[key]
		if !ok {
			return fmt.Errorf("key %q not found", key)
		}
		return c.emit("env.get", entry)
	}}
	get.Flags().StringVar(&key, "key", "", "key")
	del := &cobra.Command{Use: "delete", RunE: func(cmd *cobra.Command, args []string) error {
		if key == "" && len(args) > 0 {
			key = args[0]
		}
		if key == "" {
			return errors.New("key required (use --key or positional argument)")
		}
		v, e := c.svc.Execute(c.actor, "env.delete", "", model.EnvDeletePayload{Key: key})
		if e != nil {
			return e
		}
		return c.emit("env.delete", v)
	}}
	del.Flags().StringVar(&key, "key", "", "key")
	list := &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		return c.emit("env.list", st.Env)
	}}
	root.AddCommand(set, get, del, list)
	return root
}

func (c *cli) draftCmd() *cobra.Command {
	root := &cobra.Command{Use: "draft", Short: "Manage non-authoritative local drafts"}
	var id, kind, body, bodyFile string
	save := &cobra.Command{Use: "save", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if id == "" || kind == "" {
			return errors.New("--id and --kind are required")
		}
		if body != "" && bodyFile != "" {
			return errors.New("use only one of --body or --body-file")
		}
		var raw []byte
		var e error
		if bodyFile != "" {
			raw, e = os.ReadFile(bodyFile)
			if e != nil {
				return e
			}
		} else if body != "" {
			raw = []byte(body)
		} else {
			return errors.New("--body or --body-file is required")
		}
		if !json.Valid(raw) {
			raw, e = json.Marshal(map[string]string{"content": string(raw)})
			if e != nil {
				return e
			}
		}
		if e = c.svc.SaveDraft(id, strings.ToLower(kind), json.RawMessage(raw)); e != nil {
			return e
		}
		return c.emit("draft.save", map[string]any{"id": id, "kind": strings.ToLower(kind), "authoritative": false})
	}}
	save.Flags().StringVar(&id, "id", "", "draft ID")
	save.Flags().StringVar(&kind, "kind", "", "document, message, or artifact")
	save.Flags().StringVar(&body, "body", "", "draft JSON or text")
	save.Flags().StringVar(&bodyFile, "body-file", "", "read draft content from a file")
	var limit int
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		drafts, e := c.svc.Drafts(limit)
		if e != nil {
			return e
		}
		return c.emit("draft.list", map[string]any{"drafts": drafts, "authoritative": false})
	}}
	list.Flags().IntVar(&limit, "limit", controlplane.DefaultPageSize, "maximum drafts to return")
	root.AddCommand(save, list)
	return root
}

func (c *cli) archiveCmd() *cobra.Command {
	return &cobra.Command{Use: "archive", RunE: func(cmd *cobra.Command, args []string) error {
		v, e := c.svc.Archive(c.actor)
		if e != nil {
			return e
		}
		return c.emit("archive", v)
	}}
}
func (c *cli) exportCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{Use: "export <jsonl|markdown>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var w io.Writer = c.out
		var f *os.File
		var e error
		if out != "" {
			f, e = os.Create(out)
			if e != nil {
				return e
			}
			defer f.Close()
			w = f
		}
		switch args[0] {
		case "jsonl":
			e = c.svc.ExportJSONL(w)
		case "markdown":
			e = c.svc.ExportMarkdown(w)
		default:
			return errors.New("format must be jsonl or markdown")
		}
		return e
	}}
	cmd.Flags().StringVarP(&out, "output", "o", "", "output file")
	return cmd
}
func (c *cli) profileCmd() *cobra.Command {
	root := &cobra.Command{Use: "profile"}
	current := &cobra.Command{Use: "current", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return c.emit("profile.current", c.actorResolution)
	}}
	list := &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
		u, e := identity.LoadUserConfig()
		if e != nil {
			return e
		}
		return c.emit("profile.list", map[string]any{"active": u.ActiveProfile, "profiles": u.Profiles})
	}}
	var name string
	use := &cobra.Command{Use: "use", RunE: func(cmd *cobra.Command, args []string) error {
		u, e := identity.LoadUserConfig()
		if e != nil {
			return e
		}
		if _, ok := u.Profiles[name]; !ok {
			return errors.New("profile not found")
		}
		u.ActiveProfile = name
		if e = identity.SaveUserConfig(u); e != nil {
			return e
		}
		return c.emit("profile.use", map[string]string{"active": name})
	}}
	use.Flags().StringVar(&name, "name", "", "profile name")
	root.AddCommand(current, list, use)
	return root
}
func (c *cli) configCmd() *cobra.Command {
	return &cobra.Command{Use: "config", RunE: func(cmd *cobra.Command, args []string) error {
		u, e := identity.LoadUserConfig()
		if e != nil {
			return e
		}
		p, e := c.svc.Store.Config()
		if e != nil {
			return e
		}
		return c.emit("config", map[string]any{"user": u, "project": p, "precedence": []string{"flags", "environment", "project", "user", "defaults"}})
	}}
}
func (c *cli) themeCmd() *cobra.Command {
	root := &cobra.Command{Use: "theme", Short: "Manage UI theme"}
	var name string
	set := &cobra.Command{Use: "set", RunE: func(cmd *cobra.Command, args []string) error {
		u, e := identity.LoadUserConfig()
		if e != nil {
			return e
		}
		u.Theme = name
		if e = identity.SaveUserConfig(u); e != nil {
			return e
		}
		return c.emit("theme.set", map[string]string{"theme": name})
	}}
	set.Flags().StringVar(&name, "name", "", "theme (auto, dark, high-contrast)")
	_ = set.MarkFlagRequired("name")
	root.AddCommand(set)
	return root
}

func (c *cli) updateCmd() *cobra.Command {
	root := &cobra.Command{Use: "update"}
	var channel string
	check := &cobra.Command{Use: "check", RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
		defer cancel()
		release, err := fetchRelease(ctx, channel, "")
		if err != nil {
			return err
		}
		return c.emit("update.check", map[string]any{"current": Version, "latest": release.Tag, "channel": channel, "update_available": strings.TrimPrefix(release.Tag, "v") != Version, "telemetry": false})
	}}
	check.Flags().StringVar(&channel, "channel", "stable", "stable or preview")
	var version string
	var yes, allKnown, skipProjectUpgrade bool
	apply := &cobra.Command{Use: "apply", RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := exec.LookPath("cosign"); err != nil {
			return errors.New("verified self-update requires cosign on PATH; use the signed installer until bundled verification is available")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
		defer cancel()
		release, err := fetchRelease(ctx, channel, version)
		if err != nil {
			return err
		}
		result, err := installRelease(ctx, release)
		if err != nil {
			return err
		}
		result["binary_updated"] = true
		if skipProjectUpgrade {
			result["project_upgrade"] = map[string]any{"skipped": true, "reason": "requested by --skip-project-upgrade"}
			return c.emit("update.apply", result)
		}
		projectRoot, projectFound := currentInitializedProject(c.project)
		if allKnown || projectFound {
			upgradeResult, upgradeErr := c.handoffProjectUpgrade(ctx, result["installed"].(string), projectRoot, yes, allKnown)
			if upgradeErr != nil {
				return &projectlifecycle.Error{
					Code:    projectlifecycle.CodeUpgradeFailed,
					Message: "binary updated successfully but project reconciliation failed: " + upgradeErr.Error(),
				}
			}
			result["project_upgrade"] = upgradeResult
		} else {
			result["project_upgrade"] = map[string]any{"skipped": true, "reason": "current directory is not an initialized project"}
		}
		return c.emit("update.apply", result)
	}}
	apply.Flags().StringVar(&channel, "channel", "stable", "stable or preview")
	apply.Flags().StringVar(&version, "version", "", "exact release tag")
	apply.Flags().BoolVarP(&yes, "yes", "y", false, "approve confirmation-required project migrations")
	apply.Flags().BoolVar(&allKnown, "all-known", false, "reconcile projects recorded in identity profiles")
	apply.Flags().BoolVar(&skipProjectUpgrade, "skip-project-upgrade", false, "install the binary without reconciling projects")
	root.AddCommand(check, apply)
	return root
}

func currentInitializedProject(explicit string) (string, bool) {
	root := explicit
	if root == "" {
		root, _ = os.Getwd()
	}
	if root == "" {
		return "", false
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	info, err := os.Lstat(filepath.Join(absolute, store.Runtime))
	return absolute, err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func (c *cli) handoffProjectUpgrade(ctx context.Context, executable, projectRoot string, yes, allKnown bool) (any, error) {
	arguments := []string{"project", "upgrade"}
	if projectRoot != "" {
		arguments = append(arguments, "--project", projectRoot)
	}
	if yes {
		arguments = append(arguments, "--yes")
	}
	if allKnown {
		arguments = append(arguments, "--all-known")
	}
	process := exec.CommandContext(ctx, executable, arguments...)
	process.Stdin = os.Stdin
	if c.json {
		arguments = append(arguments, "--json", "--non-interactive")
		process = exec.CommandContext(ctx, executable, arguments...)
		var stdout, stderr bytes.Buffer
		process.Stdout = &stdout
		process.Stderr = &stderr
		if err := process.Run(); err != nil {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		var envelope Envelope
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
			return nil, fmt.Errorf("decode upgraded binary response: %w", err)
		}
		if !envelope.OK {
			return nil, errors.New("upgraded binary did not verify the project")
		}
		return envelope.Result, nil
	}
	arguments = append(arguments, "--quiet")
	process = exec.CommandContext(ctx, executable, arguments...)
	process.Stdin = os.Stdin
	process.Stdout = c.out
	process.Stderr = c.err
	if err := process.Run(); err != nil {
		return nil, err
	}
	return map[string]any{"verified": true, "all_known": allKnown}, nil
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}
type githubRelease struct {
	Tag        string         `json:"tag_name"`
	Prerelease bool           `json:"prerelease"`
	Draft      bool           `json:"draft"`
	Assets     []releaseAsset `json:"assets"`
}

func fetchRelease(ctx context.Context, channel, version string) (githubRelease, error) {
	url := "https://api.github.com/repos/DhanushSantosh/AgentComms/releases"
	if version != "" {
		url += "/tags/" + version
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return githubRelease{}, e
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return githubRelease{}, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub releases: %s", resp.Status)
	}
	if version != "" {
		var r githubRelease
		e = json.NewDecoder(resp.Body).Decode(&r)
		return r, e
	}
	var all []githubRelease
	if e = json.NewDecoder(resp.Body).Decode(&all); e != nil {
		return githubRelease{}, e
	}
	for _, r := range all {
		if !r.Draft && (channel == "preview" || !r.Prerelease) {
			return r, nil
		}
	}
	return githubRelease{}, fmt.Errorf("no %s release is available", channel)
}
func installRelease(ctx context.Context, r githubRelease) (map[string]any, error) {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	name := fmt.Sprintf("agent-comms-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext)
	urls := map[string]string{}
	for _, a := range r.Assets {
		urls[a.Name] = a.URL
	}
	for _, n := range []string{name, "checksums.txt", name + ".bundle"} {
		if urls[n] == "" {
			return nil, fmt.Errorf("release %s is missing %s", r.Tag, n)
		}
	}
	dir, e := os.MkdirTemp("", "agent-comms-update-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	for _, n := range []string{name, "checksums.txt", name + ".bundle"} {
		if e = download(ctx, urls[n], filepath.Join(dir, n)); e != nil {
			return nil, e
		}
	}
	checks, e := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if e != nil {
		return nil, e
	}
	expected := ""
	for _, line := range strings.Split(string(checks), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == name {
			expected = f[0]
		}
	}
	b, e := os.ReadFile(filepath.Join(dir, name))
	if e != nil {
		return nil, e
	}
	h := sha256.Sum256(b)
	actual := hex.EncodeToString(h[:])
	if expected == "" || actual != expected {
		return nil, errors.New("release SHA-256 verification failed")
	}
	verify := exec.CommandContext(ctx, "cosign", "verify-blob", "--bundle", filepath.Join(dir, name+".bundle"), "--certificate-identity-regexp", `^https://github.com/DhanushSantosh/AgentComms/.github/workflows/release.yml@refs/tags/`, "--certificate-oidc-issuer", "https://token.actions.githubusercontent.com", filepath.Join(dir, name))
	if out, x := verify.CombinedOutput(); x != nil {
		return nil, fmt.Errorf("cosign verification failed: %s", strings.TrimSpace(string(out)))
	}
	exe, e := os.Executable()
	if e != nil {
		return nil, e
	}
	backup := exe + ".previous"
	_ = os.Remove(backup)
	temporary, e := os.CreateTemp(filepath.Dir(exe), "."+filepath.Base(exe)+".update-*")
	if e != nil {
		return nil, e
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if e = temporary.Chmod(0o755); e == nil {
		_, e = temporary.Write(b)
	}
	if e == nil {
		e = temporary.Sync()
	}
	closeErr := temporary.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return nil, e
	}
	if e = os.Rename(exe, backup); e != nil {
		return nil, e
	}
	if e = os.Rename(temporaryPath, exe); e != nil {
		_ = os.Rename(backup, exe)
		return nil, e
	}
	if directory, openErr := os.Open(filepath.Dir(exe)); openErr == nil {
		e = directory.Sync()
		_ = directory.Close()
		if e != nil {
			return nil, e
		}
	}
	return map[string]any{"version": r.Tag, "installed": exe, "previous": backup, "verified": true}, nil
}
func download(ctx context.Context, url, path string) error {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return e
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
func (c *cli) completionCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{Use: "completion <bash|zsh|fish|powershell>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return root.GenBashCompletion(c.out)
		case "zsh":
			return root.GenZshCompletion(c.out)
		case "fish":
			return root.GenFishCompletion(c.out, true)
		case "powershell":
			return root.GenPowerShellCompletion(c.out)
		}
		return errors.New("unsupported shell")
	}}
}
func (c *cli) agentInstructionsCmd() *cobra.Command {
	return &cobra.Command{Use: "agent-instructions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		exe, _ := os.Executable()
		var registered, active bool
		var role string
		if state, e := c.svc.State(); e == nil {
			registered, active, role = onboarding.LookupAgentState(state, c.actor)
		}
		guide, e := onboarding.Render(onboarding.FromActorResolution(c.actorResolution, exe, registered, active, role))
		if e != nil {
			return e
		}
		instructions := guide + fmt.Sprintf(`
## Additional commands

  document_create: %[1]s document create --id <id> --title <title> --body <body> --tag <tag>
  decision_create:  %[1]s decision create --id <id> --title <title> --statement <statement>
  message_post:     %[1]s message post --id <id> --kind ACTION --to <actor> --subject <subject> --body <body>  (or --body-file <path> for multi-line)

Use "message post --kind CONTRACT" for binding agreements. Personal mode
coordinates one machine through SQLite; use service mode with PostgreSQL for
multi-host coordination. Git is not an authority.
`, exe)
		return c.emit("agent-instructions", map[string]any{"instructions": instructions, "binary": exe, "actor_resolution": c.actorResolution})
	}}
}
func (c *cli) mcpCmd() *cobra.Command {
	return &cobra.Command{Use: "mcp", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return mcp.Serve(c.svc, c.actorResolution, Version, os.Stdin, c.out)
	}}
}
func (c *cli) claudeCmd() *cobra.Command {
	root := &cobra.Command{Use: "claude", Short: "Serve, attach to, or tail Claude Code sessions"}
	var sessionID, projectDir string
	var noReplay bool
	tail := &cobra.Command{Use: "tail", Args: cobra.NoArgs, Short: "Stream a Claude Code session transcript live", RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(sessionID) == "" {
			return errors.New("--session is required")
		}
		dir := projectDir
		if dir == "" {
			dir = c.svc.Store.Root
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home directory: %w", err)
		}
		path, err := claudetail.SessionPath(filepath.Join(home, ".claude"), dir, sessionID)
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("no Claude session found at %s: %w", path, err)
		}
		return claudetail.Tail(cmd.Context(), path, c.out, !noReplay)
	}}
	tail.Flags().StringVar(&sessionID, "session", "", "Claude session ID to watch")
	_ = tail.MarkFlagRequired("session")
	tail.Flags().StringVar(&projectDir, "project-dir", "", "Claude Code working directory for this session (defaults to the current AgentComms project root)")
	tail.Flags().BoolVar(&noReplay, "no-replay", false, "skip replaying existing history, only show new turns")

	var listenAddress string
	serve := &cobra.Command{Use: "serve", Args: cobra.NoArgs, Short: "Run the local Claude live broker", RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if !c.quiet {
			_, _ = fmt.Fprintf(c.err, "Claude live broker listening on http://%s\n", listenAddress)
		}
		return claudeserve.Serve(ctx, listenAddress)
	}}
	serve.Flags().StringVar(&listenAddress, "listen", claudeserve.DefaultServeAddress, "loopback listen address")

	var runtimeID, serverURL string
	attach := &cobra.Command{Use: "attach", Args: cobra.NoArgs, Short: "Watch a Claude live runtime's event stream", RunE: func(cmd *cobra.Command, args []string) error {
		client := claudeserve.New(serverURL)
		events, err := client.Subscribe(cmd.Context(), runtimeID)
		if err != nil {
			return err
		}
		for event := range events {
			if rendered, ok := claudetail.Format(event); ok {
				if _, err := fmt.Fprint(c.out, rendered); err != nil {
					return err
				}
			}
		}
		return nil
	}}
	attach.Flags().StringVar(&runtimeID, "runtime", "", "registered Agent Comms runtime ID")
	_ = attach.MarkFlagRequired("runtime")
	attach.Flags().StringVar(&serverURL, "server", claudeserve.DefaultServeBaseURL(), "Claude live broker base URL")

	root.AddCommand(tail, serve, attach)
	return root
}
func (c *cli) codexCmd() *cobra.Command {
	root := &cobra.Command{Use: "codex", Short: "Serve or attach to Codex live sessions"}

	var listenAddress string
	serve := &cobra.Command{Use: "serve", Args: cobra.NoArgs, Short: "Run the local Codex live broker", RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if !c.quiet {
			_, _ = fmt.Fprintf(c.err, "Codex live broker listening on http://%s\n", listenAddress)
		}
		return codexserve.Serve(ctx, listenAddress)
	}}
	serve.Flags().StringVar(&listenAddress, "listen", codexserve.DefaultServeAddress, "loopback listen address")

	var runtimeID, serverURL string
	attach := &cobra.Command{Use: "attach", Args: cobra.NoArgs, Short: "Watch a Codex live runtime's event stream", RunE: func(cmd *cobra.Command, args []string) error {
		client := codexserve.New(serverURL)
		events, err := client.Subscribe(cmd.Context(), runtimeID)
		if err != nil {
			return err
		}
		for event := range events {
			if rendered, ok := codexserve.Format(event); ok {
				if _, err := fmt.Fprint(c.out, rendered); err != nil {
					return err
				}
			}
		}
		return nil
	}}
	attach.Flags().StringVar(&runtimeID, "runtime", "", "registered Agent Comms runtime ID")
	_ = attach.MarkFlagRequired("runtime")
	attach.Flags().StringVar(&serverURL, "server", codexserve.DefaultServeBaseURL(), "Codex live broker base URL")

	root.AddCommand(serve, attach)
	return root
}
func (c *cli) watchCmd() *cobra.Command {
	var interval time.Duration
	cmd := &cobra.Command{Use: "watch", RunE: func(cmd *cobra.Command, args []string) error {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		last := -1
		for {
			select {
			case <-cmd.Context().Done():
				return nil
			case <-tick.C:
				st, e := c.svc.State()
				if e != nil {
					return e
				}
				attention := 0
				for _, a := range st.Approvals {
					if a.Status == "PENDING" {
						attention++
					}
				}
				for _, t := range st.Tasks {
					if t.Status == "BLOCKED" || (!t.LeaseUntil.IsZero() && time.Until(t.LeaseUntil) < time.Hour) {
						attention++
					}
				}
				if attention != last {
					fmt.Fprintf(c.out, "%s attention=%d\n", time.Now().Format(time.RFC3339), attention)
					last = attention
				}
			}
		}
	}}
	cmd.Flags().DurationVar(&interval, "interval", 30*time.Second, "poll interval")
	return cmd
}
func (c *cli) tuiCmd() *cobra.Command {
	return &cobra.Command{Use: "tui", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if c.json || c.nonInteractive {
			return errors.New("tui requires an interactive terminal")
		}
		return tuiterm.Run(c.svc, c.actor, os.Stdin, c.out)
	}}
}

func (c *cli) daemonCmd() *cobra.Command {
	root := &cobra.Command{Use: "daemon", Hidden: true}
	serve := &cobra.Command{Use: "serve", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		projectRoot := c.project
		if projectRoot == "" {
			var err error
			projectRoot, err = os.Getwd()
			if err != nil {
				return err
			}
		}
		projectStore := store.Open(projectRoot)
		cfg, err := projectStore.Config()
		if err != nil {
			return err
		}
		if cfg.RuntimeMode != "service" && cfg.RuntimeMode != "personal" {
			return errors.New("daemon requires an activated personal- or service-mode project")
		}
		cachePath := runtimeinit.ProjectionPath(projectRoot)
		if cfg.RuntimeMode == "service" {
			configDir, configErr := identity.ConfigDir()
			if configErr != nil {
				return configErr
			}
			cachePath = os.Getenv("AGENT_COMMS_CACHE_PATH")
			if cachePath == "" {
				cachePath = filepath.Join(configDir, "cache.db")
			}
		}
		servicePrivateKey := ""
		if cfg.RuntimeMode == "personal" {
			credential, credentialErr := identity.ResolveCredential(projectStore.Credentials, cfg.ProjectID, "__personal_authority__")
			if credentialErr != nil {
				return fmt.Errorf("resolve personal authority signing key: %w", credentialErr)
			}
			servicePrivateKey = credential.PrivateKey
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return daemon.Run(ctx, daemon.RunConfig{
			AuthorityURL: cfg.AuthorityURL, ServicePublicKey: cfg.ServicePublicKey,
			CachePath: cachePath, Endpoint: cfg.DaemonEndpoint,
			ConnectorConfigPath: strings.TrimSpace(os.Getenv("AGENT_COMMS_CONNECTOR_CONFIG")),
			RuntimeMode:         cfg.RuntimeMode, PersonalDatabase: runtimeinit.DatabasePath(projectRoot),
			ServicePrivateKey: servicePrivateKey, ProjectID: cfg.ProjectID,
			ProductVersion: Version, BuildID: buildinfo.ResolvedBuildID(),
			ProjectFormatVersion: store.ProjectFormatVersion,
			CacheSchemaVersion:   projectlifecycle.ProjectionCacheSchemaVersion,
			DraftSchemaVersion:   projectlifecycle.DraftStoreSchemaVersion,
			DraftPath:            runtimeinit.DraftPath(projectRoot),
		})
	}}
	root.AddCommand(serve)
	return root
}

func ensureDaemon(projectRoot string, cfg store.Config) error {
	client, err := daemonclient.New(cfg.DaemonEndpoint, 300*time.Millisecond)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	health, err := client.Health(ctx)
	cancel()
	if err == nil {
		if health.RuntimeMode == cfg.RuntimeMode &&
			(health.ProjectID == cfg.ProjectID || (cfg.RuntimeMode == "service" && health.ProjectID == "*")) &&
			health.ProtocolVersion == controlplane.LocalDaemonProtocolVersion &&
			health.BuildID == buildinfo.ResolvedBuildID() &&
			health.ProjectFormatVersion == store.ProjectFormatVersion &&
			health.CacheSchemaVersion == projectlifecycle.ProjectionCacheSchemaVersion &&
			health.DraftSchemaVersion == projectlifecycle.DraftStoreSchemaVersion {
			return nil
		}
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		shutdownErr := client.Shutdown(shutdownCtx)
		shutdownCancel()
		if shutdownErr != nil {
			return fmt.Errorf("replace incompatible daemon for %s project %s: %w", health.RuntimeMode, health.ProjectID, shutdownErr)
		}
		for attempt := 0; attempt < 20; attempt++ {
			waitCtx, waitCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			_, waitErr := client.Health(waitCtx)
			waitCancel()
			if waitErr != nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	executable, executableErr := os.Executable()
	if executableErr != nil {
		return executableErr
	}
	configDir, configErr := identity.ConfigDir()
	if configErr != nil {
		return configErr
	}
	if mkdirErr := os.MkdirAll(configDir, 0o700); mkdirErr != nil {
		return mkdirErr
	}
	logFile, logErr := os.OpenFile(filepath.Join(configDir, "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if logErr != nil {
		return logErr
	}
	if startErr := launchDaemonProcess(executable, projectRoot, logFile); startErr != nil {
		_ = logFile.Close()
		return fmt.Errorf("start local daemon: %w", startErr)
	}
	_ = logFile.Close()
	for attempt := 0; attempt < 30; attempt++ {
		time.Sleep(100 * time.Millisecond)
		healthCtx, healthCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		healthErr := client.Healthy(healthCtx)
		healthCancel()
		if healthErr == nil {
			return nil
		}
	}
	return &controlplane.Error{
		Code:    controlplane.CodeUnavailable,
		Message: "local daemon did not become ready; inspect " + filepath.Join(configDir, "daemon.log"),
	}
}
