// Package onboarding is the single generator behind every surface that
// teaches a human or agent how to use Agent Comms: the project-init-time
// AGENT_INSTRUCTIONS.md file, the `agent-comms agent-instructions` CLI
// command, and the MCP `get_started` tool. They render the same decision
// tree from different Data — the static file has no live resolution yet,
// the CLI/MCP callers do — rather than three independently hand-maintained
// prose blocks drifting apart, which is what happened before this package
// existed.
package onboarding

import (
	"bytes"
	_ "embed"
	"text/template"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

//go:embed decision_tree.tmpl
var decisionTree string

var tmpl = template.Must(template.New("decision_tree").Parse(decisionTree))

const defaultBinary = "agent-comms"

// Data carries whatever is known about the current caller when rendering
// the onboarding guide. Static is true only for the project-init-time
// render, before any actor resolution or registration state exists.
type Data struct {
	Binary     string
	Actor      string
	Source     string
	Profile    string
	HostLabel  string
	ProjectID  string
	Registered bool
	Active     bool
	Role       string
	Static     bool
}

// FromActorResolution builds Data for a live render (CLI or MCP), where the
// caller has already resolved an actor via identity.ResolveActor and looked
// up its registration/activation state with LookupAgentState.
func FromActorResolution(res identity.ActorResolution, binary string, registered, active bool, role string) Data {
	return Data{
		Binary: binary, Actor: res.Actor, Source: res.Source, Profile: res.Profile,
		HostLabel: res.HostLabel, ProjectID: res.ProjectID,
		Registered: registered, Active: active, Role: role,
	}
}

// StaticData builds Data for the one render with no live resolution
// available yet: the project file written at `agent-comms init` time.
func StaticData(binary string) Data {
	return Data{Binary: binary, Static: true}
}

// Render executes the shared onboarding decision tree against d.
func Render(d Data) (string, error) {
	if d.Binary == "" {
		d.Binary = defaultBinary
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, d); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// LookupAgentState reports whether actor is a registered agent in state,
// whether it is ACTIVE, and its role. Shared by the CLI agent-instructions
// command and the MCP get_started tool so the two surfaces can't drift
// apart on how registration/activation state is derived.
func LookupAgentState(state model.State, actor string) (registered, active bool, role string) {
	agent, ok := state.Agents[actor]
	if !ok {
		return false, false, ""
	}
	return true, agent.Status == "ACTIVE", string(agent.Role)
}
