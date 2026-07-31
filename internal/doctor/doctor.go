// Package doctor computes the health findings shown by `agent-comms doctor`
// so the CLI command and the TUI's Audit & health panel share exactly one
// implementation rather than risking the two silently drifting apart.
package doctor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
	"github.com/DhanushSantosh/AgentComms/internal/daemon"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/interactiveserve"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

// Finding mirrors the shape `agent-comms doctor --json` has always emitted.
type Finding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Guidance string `json:"guidance"`
}

// Findings computes every runtime/bootstrap/state finding doctor reports,
// except the project-lifecycle ones (PROJECT_LIFECYCLE_INVALID,
// PROJECT_UPGRADE_AVAILABLE) and RUNTIME_SCHEMA_MISMATCH: those need a
// binaryVersion/buildID pair the caller already resolves for its own
// purposes (installed binary version, buildinfo.ResolvedBuildID()), so
// callers append them after calling Findings rather than passing them
// through.
func Findings(ctx context.Context, svc *service.Service) ([]Finding, error) {
	cfg, e := svc.Store.Config()
	if e != nil {
		return nil, e
	}
	var findings []Finding
	add := func(severity, code, message, guidance string) {
		findings = append(findings, Finding{severity, code, message, guidance})
	}
	if cfg.SchemaVersion != model.SchemaVersion {
		add("ERROR", "RUNTIME_SCHEMA_MISMATCH", fmt.Sprintf("binary expects schema %s but runtime is %s", model.SchemaVersion, cfg.SchemaVersion), "Use the Agent Comms version that created this project or initialize a new project.")
	}
	if cfg.ToolkitVersion == "" {
		add("WARNING", "RUNTIME_VERSION_UNKNOWN", "runtime does not record the toolkit version that created it", "Use the Agent Comms version that initialized this project.")
	} else if cfg.ToolkitVersion != buildinfo.Version {
		add("WARNING", "BINARY_RUNTIME_VERSION_MISMATCH", fmt.Sprintf("installed binary is %s but runtime was prepared by %s", buildinfo.Version, cfg.ToolkitVersion), "Install the intended release before normal work.")
	}
	if !svc.Store.ManagedBootstrapValid() {
		add("ERROR", "MANAGED_BOOTSTRAP_MISSING", "project root .agents does not match the configured authority mode", "Restore the bootstrap for this project before normal work.")
	}
	if !svc.Store.InstructionsPresent() {
		add("ERROR", "AGENT_INSTRUCTIONS_MISSING", ".agent-comms/AGENT_INSTRUCTIONS.md is missing or empty", "Restore the generated instructions with the matching Agent Comms release.")
	}
	if cfg.SchemaVersion != model.SchemaVersion {
		return findings, nil
	}
	st, x := svc.State()
	if x != nil {
		return findings, nil
	}
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
	connectorConfigPath := strings.TrimSpace(os.Getenv("AGENT_COMMS_CONNECTOR_CONFIG"))
	connectorConfigs := map[string]daemon.ConnectorConfig{}
	if connectorConfigPath != "" {
		var connectorConfigErr error
		connectorConfigs, connectorConfigErr = daemon.LoadConnectorConfigs(connectorConfigPath)
		if connectorConfigErr != nil {
			add("ERROR", "CONNECTOR_CONFIG_INVALID", connectorConfigErr.Error(),
				"Repair the private connector configuration before registering or delivering through local connectors.")
		}
	}
	localHostID, _ := identity.LoadHostID()
	interactiveByAgent := map[string]int{}
	for id, runtimeState := range st.AgentRuntimes {
		kind := runtimeState.Kind
		if kind == "" {
			kind = model.RuntimeKindWorker
		}
		if runtimeState.Connector == "LOCAL_PROCESS" || runtimeState.Connector == "WEBHOOK" {
			config, configured := connectorConfigs[runtimeState.ConfigReference]
			if runtimeState.ConfigReference == "" || connectorConfigPath == "" ||
				!configured || config.Type != runtimeState.Connector {
				add("ERROR", "RUNTIME_CONFIG_INVALID",
					fmt.Sprintf("runtime %s has no usable %s connector configuration", id, runtimeState.Connector),
					fmt.Sprintf("Drain it and run `agent-comms runtime configure --id %s` with a valid config reference.", id))
			}
		}
		if kind != model.RuntimeKindInteractive {
			continue
		}
		if runtimeState.Connector != "INTERACTIVE" || runtimeState.HostID == "" {
			add("ERROR", "INTERACTIVE_RUNTIME_MISMATCH",
				fmt.Sprintf("runtime %s is interactive but its connector or host binding is invalid", id),
				fmt.Sprintf("Drain it and run `agent-comms runtime configure --id %s --kind INTERACTIVE --connector INTERACTIVE --max-concurrent 1`.", id))
			continue
		}
		if localHostID != "" && runtimeState.HostID != localHostID {
			add("WARNING", "INTERACTIVE_RUNTIME_FOREIGN_HOST",
				fmt.Sprintf("runtime %s belongs to another host and cannot receive local PTY delivery", id),
				"Use the target host or choose a local compatible runtime explicitly.")
			continue
		}
		if runtimeState.Status == "ONLINE" {
			interactiveByAgent[runtimeState.AgentID]++
			if !interactiveserve.Alive(ctx, svc.Store.Root, id) {
				add("WARNING", "INTERACTIVE_SOCKET_UNAVAILABLE",
					fmt.Sprintf("runtime %s is governed online but its local PTY socket is not dialable", id),
					"Restart runtime interactive-serve; it will clear stale presence and establish a new endpoint.")
			}
		}
	}
	for agentID, count := range interactiveByAgent {
		if count > 1 && st.InvocationPolicies[agentID].PreferredInteractiveRuntimeID == "" {
			add("WARNING", "INTERACTIVE_RUNTIME_AMBIGUOUS",
				fmt.Sprintf("agent %s has %d online local interactive runtimes and no preferred runtime", agentID, count),
				"Set `invocation policy set --interactive-runtime` or specify `invocation request --runtime`.")
		}
	}
	for id, delivery := range st.InvocationDeliveries {
		if delivery.Status == "ATTEMPTED" && delivery.AttemptUntil != nil &&
			!delivery.AttemptUntil.After(now) {
			add("WARNING", "STALE_DELIVERY_ATTEMPT",
				fmt.Sprintf("delivery %s for invocation %s expired without a result", id, delivery.InvocationID),
				"Run a sync or targeted invocation redeliver; the daemon will record the timeout before retrying.")
		}
	}
	invocationTerminal := map[string]bool{
		"COMPLETED": true, "REJECTED": true, "EXPIRED": true,
		"CANCELLED": true, "DEAD_LETTER": true,
	}
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
	// Persistent, not one-time: `init` offers to set this up interactively,
	// but a non-interactive init (scripts, CI, tests) always skips it, and a
	// human can always decline the prompt too. Without this check the gap it
	// leaves behind -- ORCHESTRATOR grants, HUMAN-tier approvals,
	// orchestrator/human revocation, and principal deletion all protected by
	// nothing more than ordinary credential possession -- has no way to
	// resurface later.
	if owner, ok := st.Agents[cfg.Owner]; ok && owner.PrincipalType == model.PrincipalHuman && owner.ElevatedPublicKey == "" {
		add("WARNING", "NO_ELEVATED_KEY",
			"owner "+cfg.Owner+" has no elevated key registered",
			"Run `agent-comms agent elevate-key` -- without it, granting ORCHESTRATOR, approving a HUMAN-tier approval, revoking an orchestrator or human principal, and deleting a revoked principal are all protected only by ordinary credential possession, not a passphrase only you can supply.")
	}
	return findings, nil
}
