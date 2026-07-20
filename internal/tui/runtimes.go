package tui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/table"
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
		{Label: "Connector", Placeholder: "MCP", Required: true},
		{Label: "Config reference", Placeholder: "builder-local"},
		{Label: "Max concurrent", Placeholder: "1", Required: true},
		{Label: "Scopes (comma-separated)", Placeholder: "src"},
		{Label: "Capabilities (comma-separated)", Placeholder: "go,test"},
	},
	Build: func(values []string) (any, error) {
		maxConcurrent, err := strconv.Atoi(values[4])
		if err != nil {
			return nil, err
		}
		return model.RuntimeRegistered{
			AgentID: values[1], Connector: strings.ToUpper(values[2]),
			ConfigReference: values[3], MaxConcurrent: maxConcurrent,
			Scopes: splitCSV(values[5]), Capabilities: splitCSV(values[6]),
		}, nil
	},
	ResolveID: func(_ string, values []string) string { return values[0] },
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
)

type runtimeRowSource struct{ root string }

func (runtimeRowSource) Columns(width int) []table.Column {
	status, health, agent, connector, load, provider, session := 11, 10, 15, 15, 9, 9, 38
	reference := max(12, width-status-health-agent-connector-load-provider-session)
	return []table.Column{
		{Title: "STATUS", Width: status}, {Title: "HEALTH", Width: health},
		{Title: "AGENT", Width: agent}, {Title: "CONNECTOR", Width: connector},
		{Title: "LOAD", Width: load}, {Title: "PROVIDER", Width: provider},
		{Title: "SESSION / THREAD ID", Width: session}, {Title: "CONFIG", Width: reference},
	}
}

func (r runtimeRowSource) Rows(state model.State, _ string, _ bool) []table.Row {
	ids := service.SortedKeys(state.AgentRuntimes)
	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		runtime := state.AgentRuntimes[id]
		provider, session := r.sessionBinding(id)
		rows = append(rows, table.Row{
			runtime.Status, runtime.Health, runtime.AgentID, runtime.Connector,
			strconv.Itoa(len(runtime.ActiveInvocations)) + "/" + strconv.Itoa(runtime.MaxConcurrent),
			provider, session, runtime.ConfigReference,
		})
	}
	return rows
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
		actions := []RowAction{runtimeResume}
		if elevated {
			actions = append(actions, runtimeRevoke)
		}
		return actions
	case "REVOKED":
		return nil
	default:
		actions := []RowAction{runtimeDrain}
		if elevated {
			actions = append(actions, runtimeRevoke)
		}
		return actions
	}
}
