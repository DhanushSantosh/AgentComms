package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/doctor"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
	"github.com/DhanushSantosh/AgentComms/internal/runtimeinit"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/spf13/cobra"
)

// The "getting started" and "knowledge/state" command constructors
// (version, project, init, doctor, verify, status, attention, history)
// plus the shared lifecycle-transition command helpers. Split out of
// app.go.

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
