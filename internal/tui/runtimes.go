package tui

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/daemon"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/interactiveserve"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/sessionbind"
)

var runtimeRegisterForm = &ActionForm{
	Title: "Register runtime",
	Hint:  "Configuration references point to private per-user connector config; never enter a secret here.",
	Fields: []FormField{
		{Label: "Runtime ID", Placeholder: "builder-local", Required: true},
		{Label: "Agent ID", Placeholder: "builder", Required: true},
		{Label: "Kind", Options: []string{"WORKER", "INTERACTIVE"}, Required: true},
		{Label: "Connector", Options: []string{"MCP", "MANUAL", "LOCAL_PROCESS", "WEBHOOK", "QUEUE", "INTERACTIVE"}, Required: true},
		{Label: "Config reference", Placeholder: "builder-local"},
		{Label: "Max concurrent", Placeholder: "1", Required: true},
		{Label: "Scopes (comma-separated)", Placeholder: "src"},
		{Label: "Capabilities (comma-separated)", Placeholder: "go,test"},
	},
	Build: func(values []string) (any, error) {
		maxConcurrent, err := strconv.Atoi(values[5])
		if err != nil {
			return nil, err
		}
		kind := model.RuntimeKind(strings.ToUpper(values[2]))
		hostID := ""
		if kind == model.RuntimeKindInteractive || strings.EqualFold(values[3], "INTERACTIVE") {
			hostID, err = identity.LoadOrCreateHostID()
			if err != nil {
				return nil, err
			}
		}
		return model.RuntimeRegistered{
			AgentID: values[1], Kind: kind, Connector: strings.ToUpper(values[3]),
			ConfigReference: values[4], HostID: hostID, MaxConcurrent: maxConcurrent,
			Scopes: splitCSV(values[6]), Capabilities: splitCSV(values[7]),
		}, nil
	},
	ResolveID: func(_ string, values []string) string { return values[0] },
}

var runtimeConfigureForm = &ActionForm{
	Title: "Configure runtime",
	Hint:  "The runtime must be offline or draining with no active invocation.",
	Fields: []FormField{
		{Label: "Kind", Options: []string{"WORKER", "INTERACTIVE"}, Required: true},
		{Label: "Connector", Options: []string{"MANUAL", "MCP", "LOCAL_PROCESS", "WEBHOOK", "QUEUE", "INTERACTIVE"}, Required: true},
		{Label: "Config reference", Placeholder: ""},
		{Label: "Max concurrent", Placeholder: "1", Required: true},
		{Label: "Scopes (comma-separated)", Placeholder: "src"},
		{Label: "Capabilities (comma-separated)", Placeholder: "go,test"},
	},
	Build: func(values []string) (any, error) {
		maxConcurrent, err := strconv.Atoi(values[3])
		if err != nil {
			return nil, err
		}
		kind := model.RuntimeKind(strings.ToUpper(values[0]))
		connector := strings.ToUpper(values[1])
		hostID := ""
		if kind == model.RuntimeKindInteractive || connector == "INTERACTIVE" {
			hostID, err = identity.LoadOrCreateHostID()
			if err != nil {
				return nil, err
			}
		}
		return model.RuntimeConfigured{
			Kind: kind, Connector: connector, ConfigReference: values[2],
			HostID: hostID, MaxConcurrent: maxConcurrent,
			Scopes: splitCSV(values[4]), Capabilities: splitCSV(values[5]),
		}, nil
	},
}

var (
	runtimeDrain = RowAction{
		Key: "d", Label: "drain", EventType: "runtime.drain", Confirm: true,
		Payload: func() any { return model.RuntimeStatusChanged{Reason: "drained from control room"} },
		Prompt:  func(id string) string { return "Drain " + id + "? It will receive no new invocations." },
	}
	runtimeResume = RowAction{
		Key: "u", Label: "resume", EventType: "runtime.resume",
		Payload: func() any { return model.RuntimeStatusChanged{} },
	}
	runtimeRevoke = RowAction{
		Key: "x", Label: "revoke", EventType: "runtime.revoke", Confirm: true,
		Payload: func() any { return model.RuntimeStatusChanged{Reason: "revoked from control room"} },
		Prompt:  func(id string) string { return "Revoke " + id + "? This runtime cannot reconnect." },
	}
	runtimeConfigure = RowAction{
		Key: "c", Label: "configure", EventType: "runtime.configure",
		Form: runtimeConfigureForm,
	}
)

type runtimeRowSource struct{ root string }

// Columns is deliberately just the essentials -- everything else (connector,
// local PTY state, config health, load, provider/session binding, config
// reference) used to be crammed into the same table as further columns,
// unreadable outside a very wide terminal. It now lives in the detail pane
// for the selected row (see Model.runtimeDetailPane), the same
// list-plus-detail shape settings.go already uses for one row's full
// picture (settingsControl/settingsImpact).
func (runtimeRowSource) Columns(width int) []table.Column {
	status, health, kind := 11, 10, 12
	agent := max(12, width-status-health-kind)
	return []table.Column{
		{Title: "STATUS", Width: status}, {Title: "HEALTH", Width: health},
		{Title: "AGENT", Width: agent}, {Title: "KIND", Width: kind},
	}
}

func (runtimeRowSource) Rows(state model.State, _ string, _ bool) []table.Row {
	ids := service.SortedKeys(state.AgentRuntimes)
	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		runtime := state.AgentRuntimes[id]
		kind := runtime.Kind
		if kind == "" {
			kind = model.RuntimeKindWorker
		}
		rows = append(rows, table.Row{runtime.Status, runtime.Health, runtime.AgentID, string(kind)})
	}
	return rows
}

// runtimeDetail holds everything about one runtime beyond the compact table
// columns -- computed only for the currently selected row (detailFor is
// called once per render, not once per row), unlike the old design which
// probed every interactive runtime's PTY socket on every table refresh.
type runtimeDetail struct {
	connector, ptyState, configHealth, load, provider, session, configReference string
}

func (r runtimeRowSource) detailFor(id string, state model.State) (runtimeDetail, bool) {
	runtime, ok := state.AgentRuntimes[id]
	if !ok {
		return runtimeDetail{}, false
	}
	localHostID, _ := identity.LoadOrCreateHostID()
	configPath := strings.TrimSpace(os.Getenv("AGENT_COMMS_CONNECTOR_CONFIG"))
	configs, configErr := daemon.LoadConnectorConfigs(configPath)
	kind := runtime.Kind
	if kind == "" {
		kind = model.RuntimeKindWorker
	}
	ptyState := "—"
	if kind == model.RuntimeKindInteractive {
		if runtime.HostID != localHostID {
			ptyState = "foreign host"
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			alive, busy := interactiveserve.Probe(ctx, r.root, id)
			cancel()
			switch {
			case alive && busy:
				ptyState = "live · busy"
			case alive:
				ptyState = "live · idle"
			default:
				ptyState = "not dialable"
			}
		}
	}
	provider, session := r.sessionBinding(id)
	return runtimeDetail{
		connector:       runtime.Connector,
		ptyState:        ptyState,
		configHealth:    runtimeConfigurationHealth(runtime, configs, configPath, configErr),
		load:            strconv.Itoa(len(runtime.ActiveInvocations)) + "/" + strconv.Itoa(runtime.MaxConcurrent),
		provider:        provider,
		session:         session,
		configReference: runtime.ConfigReference,
	}, true
}

func runtimeConfigurationHealth(
	runtime model.AgentRuntime,
	configs map[string]daemon.ConnectorConfig,
	configPath string,
	configErr error,
) string {
	if runtime.Kind == model.RuntimeKindInteractive || runtime.Connector == "INTERACTIVE" {
		if runtime.Kind == model.RuntimeKindInteractive && runtime.Connector == "INTERACTIVE" &&
			runtime.HostID != "" && runtime.MaxConcurrent == 1 {
			return "valid"
		}
		return "mismatch"
	}
	if runtime.Connector != "LOCAL_PROCESS" && runtime.Connector != "WEBHOOK" {
		return "not required"
	}
	if configPath == "" {
		return "unavailable"
	}
	if configErr != nil {
		return "invalid file"
	}
	config, exists := configs[runtime.ConfigReference]
	if runtime.ConfigReference == "" || !exists || config.Type != runtime.Connector {
		return "missing"
	}
	return "valid"
}

// sessionBinding shows exactly which AI service provider and conversation a
// runtime is locally bound to, if any. This is local cache metadata (see
// internal/sessionbind), never the signed project event chain: it reflects
// whatever the operator's worker process last captured or was told to bind
// to, not a live, verified connection.
func (r runtimeRowSource) sessionBinding(runtimeID string) (provider, session string) {
	if r.root == "" {
		return "—", "unbound"
	}
	binding, ok, err := sessionbind.Load(r.root, runtimeID)
	if err != nil || !ok {
		return "—", "unbound"
	}
	switch binding.Adapter {
	case "claude":
		return "Claude", binding.SessionID
	case "codex":
		return "Codex", binding.SessionID
	default:
		return binding.Adapter, binding.SessionID
	}
}

func (runtimeRowSource) RowID(index int, state model.State, _ string, _ bool) string {
	ids := service.SortedKeys(state.AgentRuntimes)
	if index < 0 || index >= len(ids) {
		return ""
	}
	return ids[index]
}

func (runtimeRowSource) Actions(id string, state model.State, actor string) []RowAction {
	runtime, exists := state.AgentRuntimes[id]
	if !exists {
		return nil
	}
	principal := state.Agents[actor]
	elevated := principal.Role == model.RoleOwner || principal.Role == model.RoleOrchestrator
	if actor != runtime.AgentID && !elevated {
		return nil
	}
	switch runtime.Status {
	case "DRAINING":
		actions := []RowAction{runtimeResume, runtimeConfigure}
		if elevated {
			actions = append(actions, runtimeRevoke)
		}
		return actions
	case "REVOKED":
		return nil
	case "OFFLINE":
		actions := []RowAction{runtimeDrain, runtimeConfigure}
		if elevated {
			actions = append(actions, runtimeRevoke)
		}
		return actions
	default:
		actions := []RowAction{runtimeDrain}
		if elevated {
			actions = append(actions, runtimeRevoke)
		}
		return actions
	}
}

// runtimeDetailPane renders everything about the selected row that no
// longer fits in the compact table (runtimeRowSource.Columns) -- connector,
// local PTY dial state, config health, load, and session binding.
func (m Model) runtimeDetailPane(p palette, width int) string {
	id := m.runtimeList.SelectedID(m.state, m.actor)
	if id == "" {
		return lipgloss.NewStyle().Foreground(p.muted).Render("No runtime selected.")
	}
	detail, ok := runtimeRowSource{root: m.svc.Store.Root}.detailFor(id, m.state)
	if !ok {
		return ""
	}
	rows := []string{
		lipgloss.NewStyle().Foreground(p.violet).Bold(true).Render("RUNTIME DETAIL / " + id),
		settingLine("Connector", detail.connector),
		settingLine("Local PTY", detail.ptyState),
		settingLine("Config health", detail.configHealth),
		settingLine("Load", detail.load),
		settingLine("Provider", detail.provider),
		settingLine("Session / thread ID", empty(detail.session, "unbound")),
		settingLine("Config reference", empty(detail.configReference, "—")),
	}
	return lipgloss.NewStyle().Foreground(p.text).MaxWidth(width).
		BorderLeft(true).BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(p.violet).PaddingLeft(1).Render(strings.Join(rows, "\n"))
}
