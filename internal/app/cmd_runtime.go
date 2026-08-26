package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/interactiveserve"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/sessionbind"
	"github.com/DhanushSantosh/AgentComms/internal/terminallaunch"
	runtimeworker "github.com/DhanushSantosh/AgentComms/internal/worker"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func (c *cli) runtimeCmd() *cobra.Command {
	root := &cobra.Command{Use: "runtime", Short: "Manage agent runtime connectors and presence"}
	var agentID, runtimeKind, connector, configReference string
	var maxConcurrent int
	var scopes, capabilities []string
	register := &cobra.Command{Use: "register", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		hostID := ""
		if strings.EqualFold(runtimeKind, string(model.RuntimeKindInteractive)) ||
			strings.EqualFold(connector, "INTERACTIVE") {
			var hostErr error
			hostID, hostErr = identity.LoadOrCreateHostID()
			if hostErr != nil {
				return hostErr
			}
		}
		value, err := c.svc.Execute(c.actor, "runtime.register", id, model.RuntimeRegistered{
			AgentID: agentID, Kind: model.RuntimeKind(strings.ToUpper(runtimeKind)),
			Connector: connector, ConfigReference: configReference, HostID: hostID,
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
	register.Flags().StringVar(&runtimeKind, "kind", "WORKER", "WORKER or INTERACTIVE")
	register.Flags().StringVar(&connector, "connector", "MANUAL", "MANUAL, MCP, LOCAL_PROCESS, WEBHOOK, QUEUE, or INTERACTIVE")
	register.Flags().StringVar(&configReference, "config-reference", "", "non-secret local connector configuration reference")
	register.Flags().IntVar(&maxConcurrent, "max-concurrent", 1, "maximum concurrent invocations")
	register.Flags().StringSliceVar(&scopes, "scope", nil, "runtime scope")
	register.Flags().StringSliceVar(&capabilities, "capability", nil, "runtime capability")

	configure := &cobra.Command{Use: "configure", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		hostID := ""
		if strings.EqualFold(runtimeKind, string(model.RuntimeKindInteractive)) ||
			strings.EqualFold(connector, "INTERACTIVE") {
			var hostErr error
			hostID, hostErr = identity.LoadOrCreateHostID()
			if hostErr != nil {
				return hostErr
			}
		}
		value, err := c.svc.Execute(c.actor, "runtime.configure", id, model.RuntimeConfigured{
			Kind: model.RuntimeKind(strings.ToUpper(runtimeKind)), Connector: connector,
			ConfigReference: configReference, HostID: hostID, MaxConcurrent: maxConcurrent,
			Scopes: scopes, Capabilities: capabilities,
		})
		if err != nil {
			return err
		}
		return c.emit("runtime.configure", value)
	}}
	configure.Flags().String("id", "", "runtime ID")
	_ = configure.MarkFlagRequired("id")
	configure.Flags().StringVar(&runtimeKind, "kind", "WORKER", "WORKER or INTERACTIVE")
	configure.Flags().StringVar(&connector, "connector", "MANUAL", "MANUAL, MCP, LOCAL_PROCESS, WEBHOOK, QUEUE, or INTERACTIVE")
	configure.Flags().StringVar(&configReference, "config-reference", "", "non-secret local connector configuration reference")
	configure.Flags().IntVar(&maxConcurrent, "max-concurrent", 1, "maximum concurrent invocations")
	configure.Flags().StringSliceVar(&scopes, "scope", nil, "runtime scope")
	configure.Flags().StringSliceVar(&capabilities, "capability", nil, "runtime capability")

	var health, endpointID string
	var activeInvocations []string
	heartbeat := &cobra.Command{Use: "heartbeat", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		value, err := c.svc.Execute(c.actor, "runtime.heartbeat", id, model.RuntimeHeartbeat{
			Health: health, ActiveInvocations: activeInvocations, EndpointID: endpointID,
		})
		if err != nil {
			return err
		}
		return c.emit("runtime.heartbeat", value)
	}}
	heartbeat.Flags().String("id", "", "runtime ID")
	_ = heartbeat.MarkFlagRequired("id")
	heartbeat.Flags().StringVar(&health, "health", "HEALTHY", "HEALTHY or DEGRADED")
	heartbeat.Flags().StringVar(&endpointID, "endpoint-id", "", "opaque interactive endpoint ID")
	heartbeat.Flags().StringSliceVar(&activeInvocations, "active-invocation", nil, "active invocation ID")

	for _, operation := range []string{"drain", "resume", "revoke", "delete"} {
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
		localHostID, _ := identity.LoadHostID()
		for id, runtimeState := range state.AgentRuntimes {
			kind := runtimeState.Kind
			if kind == "" {
				kind = model.RuntimeKindWorker
				runtimeState.Kind = kind
			}
			if kind == model.RuntimeKindInteractive {
				local := localHostID != "" && runtimeState.HostID == localHostID
				session := &model.InteractiveSessionState{Local: local}
				if local {
					session.Alive, session.Busy = interactiveserve.Probe(cmd.Context(), c.svc.Store.Root, id)
					session.SocketPath = interactiveserve.SocketPath(c.svc.Store.Root, id)
				}
				runtimeState.InteractiveSession = session
			}
			state.AgentRuntimes[id] = runtimeState
		}
		headers := []string{"ID", "AGENT", "KIND", "STATUS", "HEALTH"}
		rows := make([][]string, 0, len(state.AgentRuntimes))
		for _, id := range service.SortedKeys(state.AgentRuntimes) {
			rt := state.AgentRuntimes[id]
			rows = append(rows, []string{id, rt.AgentID, string(rt.Kind), rt.Status, rt.Health})
		}
		return c.emitTable("runtime.list", state.AgentRuntimes, headers, rows)
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
						_, _ = fmt.Fprintf(c.err, "using captured %s session %s for runtime %s\n", cliui.SanitizeInline(binding.Adapter), cliui.SanitizeInline(binding.SessionID), cliui.SanitizeInline(runtimeID))
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
						_, _ = fmt.Fprintln(c.err, cliui.SanitizeInline(status))
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
			result := map[string]any{
				"runtime_id": bindSessionID, "adapter": adapter, "session_id": sessionID,
			}
			return c.emitDocument("runtime.bind-session", result, cliui.Document{
				Title: "Runtime session bound", Status: cliui.StatusSuccess,
				Fields: []cliui.Field{{Label: "Runtime", Value: bindSessionID}, {Label: "Adapter", Value: adapter}, {Label: "Session", Value: sessionID}},
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
				result := map[string]any{"runtime_id": sessionRuntimeID, "bound": false}
				return c.emitDocument("runtime.session", result, cliui.Document{Title: "Runtime session", Status: cliui.StatusWarning, Fields: []cliui.Field{{Label: "Runtime", Value: sessionRuntimeID}, {Label: "Bound", Value: "no"}}})
			}
			result := map[string]any{
				"runtime_id": sessionRuntimeID, "bound": true, "adapter": binding.Adapter,
				"session_id": binding.SessionID, "captured_at": binding.CapturedAt,
			}
			return c.emitDocument("runtime.session", result, cliui.Document{
				Title: "Runtime session", Status: cliui.StatusSuccess,
				Fields: []cliui.Field{{Label: "Runtime", Value: sessionRuntimeID}, {Label: "Bound", Value: "yes"}, {Label: "Adapter", Value: binding.Adapter}, {Label: "Session", Value: binding.SessionID}, {Label: "Captured", Value: binding.CapturedAt.Format(time.RFC3339)}},
			})
		},
	}
	sessionShow.Flags().StringVar(&sessionRuntimeID, "id", "", "runtime ID")
	_ = sessionShow.MarkFlagRequired("id")

	var interactiveServeID string
	var interactiveClaudeAllowAgentComms bool
	var interactiveLaunchTerminal bool
	var interactiveTakeoverPID int
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
			if interactiveLaunchTerminal {
				// --takeover-pid, if set, stays in the re-exec'd argv
				// (stripLaunchTerminalFlag only removes --launch-terminal)
				// and is handled for real by the freshly spawned process
				// below -- this process never touches the target pid.
				return c.launchInteractiveServeInNewTerminal()
			}
			if interactiveTakeoverPID > 0 {
				if err := interactiveserve.Takeover(interactiveTakeoverPID, interactiveserve.GracePeriod); err != nil {
					return fmt.Errorf("take over pid %d: %w", interactiveTakeoverPID, err)
				}
			}
			args = pinInteractiveServeArgs(c.svc.Store.Root, interactiveServeID, args)
			return c.runInteractiveServe(cmd.Context(), interactiveServeID, args)
		},
	}
	interactiveServe.Flags().StringVar(&interactiveServeID, "id", "", "runtime ID")
	_ = interactiveServe.MarkFlagRequired("id")
	interactiveServe.Flags().BoolVar(&interactiveClaudeAllowAgentComms, "claude-allow-agent-comms", false, "wrapped command must be claude; scopes unattended Bash permission to this Agent Comms executable only")
	interactiveServe.Flags().BoolVar(&interactiveLaunchTerminal, "launch-terminal", false, "open a new, dedicated terminal window running this same command instead of using the current one, then exit")
	interactiveServe.Flags().IntVar(&interactiveTakeoverPID, "takeover-pid", 0, "gracefully terminate this PID (an existing live session for the same provider conversation) and wait for it to fully exit before starting, so resuming it with the wrapped command's own --continue/--resume flag never collides with a still-live copy; refuses if this process is itself a descendant of pid (e.g. run from an agent's own Bash tool call) -- pair with --launch-terminal instead")

	var interactiveShowID string
	interactiveShow := &cobra.Command{
		Use:   "interactive-session",
		Short: "Show whether a runtime has a live interactive-serve session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			alive := interactiveserve.Alive(cmd.Context(), c.svc.Store.Root, interactiveShowID)
			result := map[string]any{"runtime_id": interactiveShowID, "alive": alive}
			status := cliui.StatusWarning
			if alive {
				status = cliui.StatusSuccess
			}
			return c.emitDocument("runtime.interactive-session", result, cliui.Document{Title: "Interactive runtime session", Status: status, Fields: []cliui.Field{{Label: "Runtime", Value: interactiveShowID}, {Label: "Alive", Value: fmt.Sprint(alive)}}})
		},
	}
	interactiveShow.Flags().StringVar(&interactiveShowID, "id", "", "runtime ID")
	_ = interactiveShow.MarkFlagRequired("id")

	var verifyAdapter, verifyExecutable, verifySourceDir string
	verifyFlags := &cobra.Command{
		Use:   "verify-adapter",
		Short: "Check a worker adapter's assumed CLI flags against the real installed binary's --help output",
		Long: "Statically scans the named adapter's own source file (adapter_<name>.go, under --source-dir) " +
			"for \"--flag\"-shaped string literals, runs <executable> --help, and reports any assumed " +
			"flag the real binary's own help output never mentions. Generalizes the manual check that " +
			"caught a real bug this session -- a wrong environment variable name for agy, found by " +
			"running `strings` on the installed binary by hand -- into a repeatable one for CLI flags " +
			"specifically (this can't catch environment variable drift; --help never lists those). " +
			"A dev-time tool: --source-dir must point at a real checkout's internal/worker directory " +
			"(defaults to that path relative to the current directory, i.e. running from the repo root), " +
			"not something a distributed binary carries with it.",
		Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			missing, e := runtimeworker.VerifyAdapterFlags(cmd.Context(), verifyAdapter, verifySourceDir, verifyExecutable)
			if e != nil {
				return e
			}
			result := map[string]any{
				"adapter": verifyAdapter, "executable": verifyExecutable,
				"missing_flags": missing, "clean": len(missing) == 0,
			}
			status := cliui.StatusSuccess
			if len(missing) > 0 {
				status = cliui.StatusWarning
			}
			return c.emitDocument("runtime.verify-adapter", result, cliui.Document{
				Title: "Adapter verification", Status: status,
				Fields: []cliui.Field{{Label: "Adapter", Value: verifyAdapter}, {Label: "Executable", Value: verifyExecutable}, {Label: "Missing flags", Value: strings.Join(missing, ", ")}, {Label: "Clean", Value: fmt.Sprint(len(missing) == 0)}},
			})
		},
	}
	verifyFlags.Flags().StringVar(&verifyAdapter, "adapter", "", "adapter name (claude, codex, opencode)")
	_ = verifyFlags.MarkFlagRequired("adapter")
	verifyFlags.Flags().StringVar(&verifyExecutable, "executable", "", "path to the real installed CLI binary")
	_ = verifyFlags.MarkFlagRequired("executable")
	verifyFlags.Flags().StringVar(&verifySourceDir, "source-dir", filepath.Join("internal", "worker"), "path to the agent-comms repo checkout's internal/worker directory")

	root.AddCommand(register, configure, heartbeat, list, workerCommand, bindSession, sessionShow,
		interactiveServe, interactiveShow, verifyFlags)
	return root
}

// stripLaunchTerminalFlag removes the --launch-terminal flag (in either its
// bare or --launch-terminal=<value> form) from args, so re-execing the same
// invocation inside a freshly opened terminal doesn't recurse into another
// launch instead of actually serving.
func stripLaunchTerminalFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--launch-terminal" || strings.HasPrefix(a, "--launch-terminal=") {
			continue
		}
		out = append(out, a)
	}
	return out
}

// launchInteractiveServeInNewTerminal re-execs the exact command line the
// operator just typed -- minus --launch-terminal itself -- inside a freshly
// opened, dedicated terminal window via internal/terminallaunch, then
// returns immediately. This exists only to remove the manual "open a
// terminal, cd here, retype the long command" step; the resulting session
// still needs a real, dedicated window for the same reason the plain
// command always has (see docs/agent-invocations.md's interactive-serve
// section) -- this does not, and structurally cannot, relax that.
func (c *cli) launchInteractiveServeInNewTerminal() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve agent-comms executable: %w", err)
	}
	full := append([]string{executable}, stripLaunchTerminalFlag(os.Args[1:])...)
	if err := terminallaunch.Open(c.svc.Store.Root, full); err != nil {
		return fmt.Errorf("open a new terminal window: %w -- run this command yourself instead: %s", err, strings.Join(full, " "))
	}
	result := map[string]any{"command": full, "working_directory": c.svc.Store.Root}
	return c.emitDocument("runtime.interactive-serve-launched", result, cliui.Document{
		Title: "Interactive runtime launched", Status: cliui.StatusSuccess,
		Fields: []cliui.Field{{Label: "Command", Value: strings.Join(full, " ")}, {Label: "Working directory", Value: c.svc.Store.Root}},
	})
}

// pinInteractiveServeArgs rewrites args to explicitly resume a previously
// pinned session/conversation ID for runtimeID, if sessionbind's local
// cache has one, in place of whatever implicit "most recent" flag the
// command line happened to spell out -- this is what makes resuming the
// exact same conversation deterministic across a --takeover-pid
// kill/respawn instead of racing each provider CLI's own recency-based
// lookup. A runtime with no binding yet (its first-ever start) is returned
// unchanged. Extracted from interactiveServe's RunE (like
// withClaudeAllowAgentComms below) so this can be tested directly without
// needing a real pty.
func pinInteractiveServeArgs(projectRoot, runtimeID string, args []string) []string {
	binding, ok, err := sessionbind.Load(projectRoot, runtimeID)
	if err != nil || !ok {
		return args
	}
	return interactiveserve.PinResumeArgs(args, binding.SessionID)
}

func (c *cli) runInteractiveServe(ctx context.Context, runtimeID string, command []string) (runErr error) {
	hostID, err := identity.LoadOrCreateHostID()
	if err != nil {
		return fmt.Errorf("load local host identity: %w", err)
	}
	state, err := c.svc.State()
	if err != nil {
		return err
	}
	runtimeState, exists := state.AgentRuntimes[runtimeID]
	if !exists {
		if !c.quiet {
			fmt.Fprintf(c.err, "runtime %q not found; auto-registering as INTERACTIVE with agent %q\n", cliui.SanitizeInline(runtimeID), cliui.SanitizeInline(c.actor))
		}
		if _, err = c.svc.Execute(c.actor, "runtime.register", runtimeID, model.RuntimeRegistered{
			AgentID: c.actor, Kind: model.RuntimeKindInteractive,
			Connector: "INTERACTIVE", HostID: hostID, MaxConcurrent: 1,
		}); err != nil {
			return fmt.Errorf("register interactive runtime: %w", err)
		}
	} else {
		kind := runtimeState.Kind
		if kind == "" {
			kind = model.RuntimeKindWorker
		}
		if runtimeState.AgentID != c.actor || kind != model.RuntimeKindInteractive ||
			runtimeState.Connector != "INTERACTIVE" || runtimeState.HostID != hostID {
			return fmt.Errorf(
				"runtime %s is not this actor's local INTERACTIVE runtime; repair it while offline with `agent-comms --actor %s runtime configure --id %s --kind INTERACTIVE --connector INTERACTIVE --max-concurrent 1`",
				runtimeID, c.actor, runtimeID,
			)
		}
		if runtimeState.Status == "DRAINING" {
			return fmt.Errorf("runtime %s is draining; run `agent-comms --actor %s runtime resume --id %s` before starting it", runtimeID, c.actor, runtimeID)
		}
		if runtimeState.Status == "REVOKED" {
			return fmt.Errorf("runtime %s is revoked and cannot start", runtimeID)
		}
		if interactiveserve.Alive(ctx, c.svc.Store.Root, runtimeID) {
			return fmt.Errorf("runtime %s already has a live interactive session", runtimeID)
		}
		if runtimeState.Status == "ONLINE" {
			if _, err = c.svc.Execute(c.actor, "runtime.offline", runtimeID,
				model.RuntimeStatusChanged{
					Reason: "replacing a stale interactive session", EndpointID: runtimeState.EndpointID,
				}); err != nil {
				return fmt.Errorf("clear stale interactive runtime presence: %w", err)
			}
		}
	}
	endpointID := uuid.NewString()
	if err = c.heartbeatInteractiveRuntime(runtimeID, endpointID); err != nil {
		return fmt.Errorf("start interactive runtime heartbeat: %w", err)
	}
	serveCtx, cancel := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interactiveHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-serveCtx.Done():
				heartbeatDone <- nil
				return
			case <-ticker.C:
				if heartbeatErr := c.heartbeatInteractiveRuntime(runtimeID, endpointID); heartbeatErr != nil {
					heartbeatDone <- heartbeatErr
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		cancel()
		heartbeatErr := <-heartbeatDone
		_, offlineErr := c.svc.Execute(c.actor, "runtime.offline", runtimeID,
			model.RuntimeStatusChanged{Reason: "interactive session exited", EndpointID: endpointID})
		if runErr == nil && heartbeatErr != nil {
			runErr = fmt.Errorf("interactive runtime heartbeat: %w", heartbeatErr)
		}
		if runErr == nil && offlineErr != nil {
			runErr = fmt.Errorf("mark interactive runtime offline: %w", offlineErr)
		}
	}()
	code, err := interactiveserve.Serve(serveCtx, interactiveserve.ServeOptions{
		ProjectRoot: c.svc.Store.Root, RuntimeID: runtimeID, Command: command, Actor: c.actor,
		OnStarted: func(pid int) {
			if len(command) == 0 {
				return
			}
			adapter := filepath.Base(command[0])
			if sessionID, ok := discoverSessionID(adapter, pid, c.svc.Store.Root); ok {
				_ = sessionbind.Save(c.svc.Store.Root, runtimeID, sessionID, adapter)
			}
		},
	})
	if err != nil {
		return err
	}
	c.processExitCode = code
	return nil
}

func (c *cli) heartbeatInteractiveRuntime(runtimeID, endpointID string) error {
	state, err := c.svc.State()
	if err != nil {
		return err
	}
	activeInvocations := make([]string, 0)
	for _, invocation := range state.Invocations {
		if invocation.RuntimeID == runtimeID &&
			(invocation.Status == "CLAIMED" || invocation.Status == "RUNNING" ||
				invocation.Status == "WAITING") {
			activeInvocations = append(activeInvocations, invocation.ID)
		}
	}
	sort.Strings(activeInvocations)
	_, err = c.svc.Execute(c.actor, "runtime.heartbeat", runtimeID, model.RuntimeHeartbeat{
		Health: "HEALTHY", EndpointID: endpointID,
		ActiveInvocations: activeInvocations,
	})
	return err
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
