package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
	"github.com/DhanushSantosh/AgentComms/internal/claudeserve"
	"github.com/DhanushSantosh/AgentComms/internal/claudetail"
	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/codexserve"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/daemon"
	"github.com/DhanushSantosh/AgentComms/internal/daemonclient"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/mcp"
	"github.com/DhanushSantosh/AgentComms/internal/onboarding"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
	"github.com/DhanushSantosh/AgentComms/internal/runtimeinit"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	tuiterm "github.com/DhanushSantosh/AgentComms/internal/tui"
	"github.com/spf13/cobra"
)

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
		result := map[string]any{"instructions": instructions, "binary": exe, "actor_resolution": c.actorResolution}
		if c.json {
			return c.emit("agent-instructions", result)
		}
		mode := cliui.Mode(c.output)
		if mode == "" {
			mode = cliui.ModeHuman
		}
		if c.quiet {
			return nil
		}
		return (cliui.Presenter{Out: c.out, Mode: mode, Capabilities: cliui.DetectCapabilities(c.out, c.noColor)}).RenderText("Agent instructions", instructions)
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
			_, _ = fmt.Fprintf(c.err, "Claude live broker listening on http://%s\n", cliui.SanitizeInline(listenAddress))
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
			_, _ = fmt.Fprintf(c.err, "Codex live broker listening on http://%s\n", cliui.SanitizeInline(listenAddress))
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
	var count int
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
					if c.jsonl {
						if err := c.emitStream("watch", "attention.changed", map[string]any{"attention": attention, "previous": last}); err != nil {
							return err
						}
					} else {
						fmt.Fprintf(c.out, "%s attention=%d\n", time.Now().UTC().Format(time.RFC3339), attention)
					}
					last = attention
					if count > 0 {
						count--
						if count == 0 {
							return nil
						}
					}
				}
			}
		}
	}}
	cmd.Flags().DurationVar(&interval, "interval", 30*time.Second, "poll interval")
	cmd.Flags().IntVar(&count, "count", 0, "stop after this many changes (0 = keep watching)")
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
			ProjectRoot:          projectRoot,
		})
	}}
	root.AddCommand(serve)
	return root
}

func ensureDaemon(projectRoot string, cfg store.Config) error {
	client, err := daemonclient.New(cfg.DaemonEndpoint, daemonHealthRequestTimeout)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), daemonHealthRequestTimeout)
	health, err := client.Health(ctx)
	cancel()
	if err == nil {
		if health.RuntimeMode == cfg.RuntimeMode &&
			(health.ProjectID == cfg.ProjectID || (cfg.RuntimeMode == "service" && health.ProjectID == "*")) &&
			health.ProtocolVersion == controlplane.LocalDaemonProtocolVersion &&
			health.ProductVersion == Version &&
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
		for attempt := 0; attempt < daemonShutdownWaitAttempts; attempt++ {
			waitCtx, waitCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			_, waitErr := client.Health(waitCtx)
			waitCancel()
			if waitErr != nil {
				break
			}
			time.Sleep(daemonShutdownWaitSleep)
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
	readyDeadline := time.Now().Add(daemonReadyTimeout)
	for time.Now().Before(readyDeadline) {
		time.Sleep(daemonReadyPollInterval)
		healthCtx, healthCancel := context.WithTimeout(context.Background(), daemonHealthRequestTimeout)
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
