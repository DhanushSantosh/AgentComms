package tui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/table"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
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

type runtimeRowSource struct{}

func (runtimeRowSource) Columns(width int) []table.Column {
	status, health, agent, connector, load := 11, 10, 15, 15, 9
	reference := max(12, width-status-health-agent-connector-load)
	return []table.Column{
		{Title: "STATUS", Width: status}, {Title: "HEALTH", Width: health},
		{Title: "AGENT", Width: agent}, {Title: "CONNECTOR", Width: connector},
		{Title: "LOAD", Width: load}, {Title: "CONFIG", Width: reference},
	}
}

func (runtimeRowSource) Rows(state model.State, _ string, _ bool) []table.Row {
	ids := service.SortedKeys(state.AgentRuntimes)
	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		runtime := state.AgentRuntimes[id]
		rows = append(rows, table.Row{
			runtime.Status, runtime.Health, runtime.AgentID, runtime.Connector,
			strconv.Itoa(len(runtime.ActiveInvocations)) + "/" + strconv.Itoa(runtime.MaxConcurrent),
			runtime.ConfigReference,
		})
	}
	return rows
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
