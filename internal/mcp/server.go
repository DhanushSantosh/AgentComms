package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/failure"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/onboarding"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/google/uuid"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
type callParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ProtocolVersion is the MCP protocol version advertised by this server.
const ProtocolVersion = "2025-06-18"

// tool builds one MCP tool descriptor. required defaults to a nil slice
// (Go's zero value for a variadic called with no arguments), which
// json.Marshal renders as "required":null — invalid per JSON Schema, which
// requires "required" to be an array when present. Confirmed live: Claude
// Code's MCP client fetches tools/list successfully but silently rejects
// the whole response ("tools fetch failed" with no further detail) when
// any tool schema has "required":null, since every no-required-args tool
// (status, history, invocation_next, verify) produced exactly that.
func tool(name, description string, properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"name": name, "description": description, "inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}
}
func tools() []map[string]any {
	return []map[string]any{
		tool("identity", "Show the actor identity bound to this MCP connection: actor, how it was resolved, and the project ID", map[string]any{}),
		tool("get_started", "Learn how to participate in this project right now: your current identity/registration state and the exact next steps", map[string]any{}),
		tool("project_upgrade_status", "Read project compatibility and pending maintenance; this tool never applies an upgrade", map[string]any{}),
		tool("status", "Read the governed project state", map[string]any{}),
		tool("history", "Read a bounded page of immutable signed events", map[string]any{
			"cursor": map[string]any{"type": "string"},
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": controlplane.MaxPageSize},
		}),
		tool("task_create", "Create a coordination task", map[string]any{"id": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "repository": map[string]any{"type": "string"}, "branch": map[string]any{"type": "string"}, "resources": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "id", "title", "repository", "branch", "resources"),
		tool("task_claim", "Claim an open task with a protected lease", map[string]any{"id": map[string]any{"type": "string"}}, "id"),
		tool("message_post", "Post a typed durable message", map[string]any{"id": map[string]any{"type": "string"}, "kind": map[string]any{"type": "string", "enum": []string{"FYI", "ACTION", "CONTRACT", "BLOCKER", "DECISION"}}, "to": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "subject": map[string]any{"type": "string"}, "body": map[string]any{"type": "string"}}, "id", "kind", "to", "subject"),
		tool("invocation_request", "Request that another agent runtime act on bounded instructions", map[string]any{
			"id": map[string]any{"type": "string"}, "target": map[string]any{"type": "string"},
			"instruction":     map[string]any{"type": "string", "maxLength": controlplane.MaxInvocationBytes},
			"expected_result": map[string]any{"type": "string"}, "message_id": map[string]any{"type": "string"},
			"task_id":              map[string]any{"type": "string"},
			"scopes":               map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"priority":             map[string]any{"type": "string", "enum": []string{"LOW", "NORMAL", "HIGH", "URGENT"}},
			"consumer_mode":        map[string]any{"type": "string", "enum": []string{"INTERACTIVE_ONLY", "WORKER_ONLY", "EITHER"}},
			"preferred_runtime_id": map[string]any{"type": "string"},
		}, "id", "target", "instruction"),
		tool("invocation_get", "Read one invocation with its delivery evidence and target acknowledgement", map[string]any{
			"id": map[string]any{"type": "string"},
		}, "id"),
		tool("invocation_redeliver", "Explicitly retry delivery of an unclaimed open invocation to one runtime", map[string]any{
			"id": map[string]any{"type": "string"}, "runtime_id": map[string]any{"type": "string"},
		}, "id", "runtime_id"),
		tool("invocation_policy_get", "Read the invocation policy and consumer-routing defaults for one agent", map[string]any{
			"agent_id": map[string]any{"type": "string"},
		}, "agent_id"),
		tool("invocation_policy_set", "Govern who may invoke an agent and which runtime kinds may consume its work", map[string]any{
			"agent_id":              map[string]any{"type": "string"},
			"mode":                  map[string]any{"type": "string", "enum": []string{"MANUAL", "TRUSTED", "AUTOMATIC", "DISABLED"}},
			"trusted_actors":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"allowed_scopes":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"default_consumer_mode": map[string]any{"type": "string", "enum": []string{"INTERACTIVE_ONLY", "WORKER_ONLY", "EITHER"}},
			"allowed_consumer_modes": map[string]any{"type": "array", "items": map[string]any{
				"type": "string", "enum": []string{"INTERACTIVE_ONLY", "WORKER_ONLY", "EITHER"},
			}},
			"preferred_interactive_runtime_id": map[string]any{"type": "string"},
			"require_human_for_sensitive":      map[string]any{"type": "boolean"},
		}, "agent_id", "mode"),
		tool("invocation_next", "Read the next claimable invocation for this agent", map[string]any{
			"runtime_id": map[string]any{"type": "string"},
		}),
		tool("invocation_listen", "Wait for and optionally claim the next invocation pushed to this connected runtime", map[string]any{
			"runtime_id":   map[string]any{"type": "string"},
			"wait_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": int(controlplane.MaxInvocationListen / time.Second)},
			"auto_claim":   map[string]any{"type": "boolean", "default": true},
		}, "runtime_id"),
		tool("invocation_claim", "Exclusively claim an invocation for a runtime", map[string]any{
			"id": map[string]any{"type": "string"}, "runtime_id": map[string]any{"type": "string"},
		}, "id", "runtime_id"),
		tool("invocation_start", "Mark a claimed invocation as running", map[string]any{
			"id": map[string]any{"type": "string"}, "summary": map[string]any{"type": "string"},
		}, "id"),
		tool("invocation_wait", "Mark a running invocation as waiting for input", map[string]any{
			"id": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"},
		}, "id", "reason"),
		tool("invocation_resume", "Resume a waiting invocation", map[string]any{
			"id": map[string]any{"type": "string"}, "summary": map[string]any{"type": "string"},
		}, "id"),
		tool("invocation_complete", "Complete an invocation with a bounded result summary", map[string]any{
			"id": map[string]any{"type": "string"}, "summary": map[string]any{"type": "string"},
			"result_message_id": map[string]any{"type": "string"},
		}, "id", "summary"),
		tool("invocation_reject", "Reject an open invocation with a reason", map[string]any{
			"id": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"},
		}, "id", "reason"),
		tool("agent_register", "Register a new agent principal, generating its own fresh signing keypair. Any connection may always self-register (id equal to this connection's own actor). Registering a different id requires this connection's actor to be an active orchestrator or human principal — otherwise rejected.", map[string]any{
			"id":             map[string]any{"type": "string"},
			"display_name":   map[string]any{"type": "string"},
			"principal_type": map[string]any{"type": "string", "enum": []string{"HUMAN", "AGENT"}},
		}, "id"),
		tool("agent_activate", "Activate a registered agent with a role and scopes. Requires elevated (owner or approved) authorization, exactly like `agent-comms agent activate` — this does not loosen that rule, only exposes it over MCP.", map[string]any{
			"id":           map[string]any{"type": "string"},
			"role":         map[string]any{"type": "string"},
			"capabilities": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"scopes":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, "id", "role"),
		tool("agent_switch_role", "Switch your own role (self-service, never someone else's) to ORCHESTRATOR or any freeform custom label — never OWNER. Never touches your capabilities or scopes. A switch to ORCHESTRATOR requires a human principal, a pre-approved HUMAN-tier approval, and the elevated key — which MCP cannot supply, so that specific switch is refused here exactly like `agent_activate` already refuses it; use the CLI instead.", map[string]any{
			"role": map[string]any{"type": "string"},
		}, "role"),
		tool("agent_revoke", "Permanently revoke an agent principal — terminal, cannot be reversed or reactivated. The owner principal cannot be revoked. Revoking an orchestrator or any human principal requires an active human principal to call this, unless revoking yourself.", map[string]any{
			"id":     map[string]any{"type": "string"},
			"reason": map[string]any{"type": "string"},
		}, "id"),
		tool("runtime_register", "Register an agent runtime without embedding connector secrets", map[string]any{
			"id": map[string]any{"type": "string"}, "connector": map[string]any{"type": "string"},
			"kind":             map[string]any{"type": "string", "enum": []string{"WORKER", "INTERACTIVE"}},
			"config_reference": map[string]any{"type": "string"}, "max_concurrent": map[string]any{"type": "integer", "minimum": 1, "maximum": controlplane.MaxRuntimeConcurrency},
			"scopes":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"capabilities": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, "id", "connector", "max_concurrent"),
		tool("runtime_configure", "Repair a stopped runtime's governed kind, connector, and bounded capabilities", map[string]any{
			"id": map[string]any{"type": "string"}, "connector": map[string]any{"type": "string"},
			"kind":             map[string]any{"type": "string", "enum": []string{"WORKER", "INTERACTIVE"}},
			"config_reference": map[string]any{"type": "string"},
			"max_concurrent":   map[string]any{"type": "integer", "minimum": 1, "maximum": controlplane.MaxRuntimeConcurrency},
			"scopes":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"capabilities":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, "id", "connector", "max_concurrent"),
		tool("runtime_heartbeat", "Update this agent runtime's bounded presence record", map[string]any{
			"id": map[string]any{"type": "string"}, "health": map[string]any{"type": "string", "enum": []string{"HEALTHY", "DEGRADED"}},
			"active_invocations": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"endpoint_id":        map[string]any{"type": "string"},
		}, "id", "health"),
		tool("verify", "Verify signatures and hash-chain integrity", map[string]any{}),
	}
}

// Serve runs the stdio MCP loop for one connection, bound to resolution's
// actor for every tool call. resolution is threaded through (rather than a
// bare actor string) so the "identity" and "get_started" tools can report
// how the actor was resolved, not just what it is — the same
// identity.ActorResolution the CLI's `profile current` already exposes.
func Serve(s *service.Service, resolution identity.ActorResolution, serverVersion string, in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 64*1024), controlplane.MaxCommandBytes)
	enc := json.NewEncoder(out)
	for scan.Scan() {
		var q request
		if e := json.Unmarshal(scan.Bytes(), &q); e != nil {
			_ = enc.Encode(response{JSONRPC: "2.0", ID: nil, Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		r, ok := handle(s, resolution, serverVersion, q)
		if !ok {
			continue
		}
		if e := enc.Encode(r); e != nil {
			return e
		}
	}
	return scan.Err()
}

// handle dispatches one request and reports whether a response should be
// sent at all. Per JSON-RPC 2.0, a notification (by MCP convention, any
// method under "notifications/") never receives a response — sending one
// anyway is spec non-compliance that could confuse a strict client's
// response correlation, even though lenient clients tolerate it.
func handle(s *service.Service, resolution identity.ActorResolution, serverVersion string, q request) (response, bool) {
	if strings.HasPrefix(q.Method, "notifications/") {
		return response{}, false
	}
	r := response{JSONRPC: "2.0", ID: q.ID}
	switch q.Method {
	case "initialize":
		r.Result = map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "agent-comms", "version": serverVersion}}
	case "tools/list":
		r.Result = map[string]any{"tools": tools()}
	case "tools/call":
		var p callParams
		if e := json.Unmarshal(q.Params, &p); e != nil {
			return rpcFail(r, -32602, e), true
		}
		v, e := call(s, resolution, p)
		if e != nil {
			return rpcFail(r, -32000, e), true
		}
		b, _ := json.Marshal(v)
		r.Result = map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}, "structuredContent": v}
	default:
		return rpcFail(r, -32601, fmt.Errorf("method %s not found", q.Method)), true
	}
	return r, true
}
func rpcFail(r response, code int, e error) response {
	r.Error = &rpcError{
		Code: code, Message: e.Error(),
		Data: map[string]any{"code": failure.Code(e)},
	}
	return r
}

// No MCP resources/* support is implemented. Considered for surfacing the
// onboarding guide, but rejected: tool-calling is the one capability
// already confirmed live across every MCP host this project targets,
// whereas resource support is inconsistent across hosts and is often
// surfaced to a human picker rather than auto-read by the model — the
// wrong shape for an agent to self-orient with zero human involvement. The
// "get_started" tool below is that entry point instead.
func call(s *service.Service, resolution identity.ActorResolution, p callParams) (any, error) {
	actor := resolution.Actor
	switch p.Name {
	case "identity":
		return resolution, nil
	case "get_started":
		state, e := s.State()
		if e != nil {
			return nil, e
		}
		registered, active, role := onboarding.LookupAgentState(state, actor)
		guide, e := onboarding.Render(onboarding.FromActorResolution(resolution, "agent-comms", registered, active, role))
		if e != nil {
			return nil, e
		}
		compatibility, _, compatibilityErr := projectlifecycle.Inspect(s.Store.Root, buildinfo.Version, buildinfo.ResolvedBuildID())
		if compatibilityErr != nil {
			return nil, compatibilityErr
		}
		return map[string]any{
			"identity": resolution, "registered": registered, "active": active, "role": role, "guide": guide,
			"project_compatibility": compatibility,
		}, nil
	case "project_upgrade_status":
		plan, _, e := projectlifecycle.Inspect(s.Store.Root, buildinfo.Version, buildinfo.ResolvedBuildID())
		return plan, e
	case "status":
		return s.State()
	case "history":
		limit := 0
		if raw, ok := p.Arguments["limit"].(float64); ok {
			limit = int(raw)
		}
		return s.History(controlplane.PageRequest{Cursor: stringArg(p.Arguments, "cursor"), Limit: limit})
	case "verify":
		if e := s.Verify(0, 0); e != nil {
			return nil, e
		}
		state, e := s.State()
		if e != nil {
			return nil, e
		}
		return map[string]any{"verified": true, "head": state.Integrity.Head, "consistency": state.Integrity.Consistency}, nil
	case "agent_register":
		id := stringArg(p.Arguments, "id")
		if id != actor {
			can, canErr := s.CanSponsorRegistration(actor)
			if canErr != nil {
				return nil, canErr
			}
			if !can {
				return nil, fmt.Errorf("agent_register: registering a different id requires an active orchestrator or human principal (actor: %s)", actor)
			}
			// actor is an active orchestrator or human principal (which
			// includes the project owner by construction, covering the
			// original owner-fallback bootstrap case: a brand-new connection
			// with no dedicated identity yet resolves to the owner, who
			// already qualifies here) -- this is sponsoring a brand-new
			// identity's own self-signed registration, never impersonating
			// an existing one (Register always mints id's own fresh keypair).
		}
		principalType := stringArg(p.Arguments, "principal_type")
		if principalType == "" {
			principalType = "AGENT"
		}
		if principalType != string(model.PrincipalHuman) && principalType != string(model.PrincipalAgent) {
			return nil, fmt.Errorf("agent_register: principal_type must be HUMAN or AGENT")
		}
		return s.Register(id, stringArg(p.Arguments, "display_name"), model.PrincipalType(principalType))
	case "agent_activate":
		return s.Execute(actor, "agent.activate", stringArg(p.Arguments, "id"), model.AgentActivated{
			Role: model.Role(stringArg(p.Arguments, "role")), Capabilities: stringsArg(p.Arguments["capabilities"]),
			Scopes: stringsArg(p.Arguments["scopes"]),
		})
	case "agent_switch_role":
		return s.Execute(actor, "agent.switch-role", actor, model.AgentRoleSwitched{
			Role: model.Role(stringArg(p.Arguments, "role")),
		})
	case "agent_revoke":
		return s.Execute(actor, "agent.revoke", stringArg(p.Arguments, "id"),
			model.RuntimeStatusChanged{Reason: stringArg(p.Arguments, "reason")})
	case "task_create":
		id, _ := p.Arguments["id"].(string)
		res := stringsArg(p.Arguments["resources"])
		return s.Execute(actor, "task.create", id, model.TaskCreated{Title: stringArg(p.Arguments, "title"), Repository: stringArg(p.Arguments, "repository"), Branch: stringArg(p.Arguments, "branch"), Resources: res})
	case "task_claim":
		return s.Execute(actor, "task.claim", stringArg(p.Arguments, "id"), model.TaskClaimed{})
	case "message_post":
		return s.Execute(actor, "message.post", stringArg(p.Arguments, "id"), model.MessagePosted{Kind: stringArg(p.Arguments, "kind"), To: stringsArg(p.Arguments["to"]), Subject: stringArg(p.Arguments, "subject"), Body: stringArg(p.Arguments, "body")})
	case "invocation_request":
		invocationID := stringArg(p.Arguments, "id")
		event, err := s.Execute(actor, "invocation.request", invocationID, model.InvocationRequested{
			Target: stringArg(p.Arguments, "target"), MessageID: stringArg(p.Arguments, "message_id"),
			TaskID: stringArg(p.Arguments, "task_id"), Instruction: stringArg(p.Arguments, "instruction"),
			ExpectedResult: stringArg(p.Arguments, "expected_result"),
			Scopes:         stringsArg(p.Arguments["scopes"]), Priority: stringArg(p.Arguments, "priority"),
			ConsumerMode:       model.ConsumerMode(stringArg(p.Arguments, "consumer_mode")),
			PreferredRuntimeID: stringArg(p.Arguments, "preferred_runtime_id"),
		})
		if err != nil {
			return nil, err
		}
		state, err := s.State()
		if err != nil {
			return nil, err
		}
		rawEvent, err := json.Marshal(event)
		if err != nil {
			return nil, err
		}
		result := map[string]any{}
		if err = json.Unmarshal(rawEvent, &result); err != nil {
			return nil, err
		}
		localHostID, _ := identity.LoadHostID()
		delivery, exists := service.SummarizeInvocationDelivery(state, invocationID, "", localHostID)
		if !exists {
			return nil, fmt.Errorf("invocation %s not found after commit", invocationID)
		}
		result["delivery"] = delivery
		return result, nil
	case "invocation_get":
		invocationID := stringArg(p.Arguments, "id")
		state, err := s.State()
		if err != nil {
			return nil, err
		}
		invocation, exists := state.Invocations[invocationID]
		if !exists {
			return nil, fmt.Errorf("invocation %s not found", invocationID)
		}
		deliveries := make([]model.InvocationDelivery, 0)
		for _, delivery := range state.InvocationDeliveries {
			if delivery.InvocationID == invocationID {
				deliveries = append(deliveries, delivery)
			}
		}
		sort.Slice(deliveries, func(left, right int) bool {
			return deliveries[left].Attempt < deliveries[right].Attempt
		})
		return map[string]any{
			"invocation": invocation, "deliveries": deliveries,
			"target_acknowledged": invocation.ClaimedAt != nil,
			"acknowledged_at":     invocation.ClaimedAt,
		}, nil
	case "invocation_redeliver":
		invocationID := stringArg(p.Arguments, "id")
		runtimeID := stringArg(p.Arguments, "runtime_id")
		state, err := s.State()
		if err != nil {
			return nil, err
		}
		invocation, exists := state.Invocations[invocationID]
		if !exists {
			return nil, fmt.Errorf("invocation %s not found", invocationID)
		}
		if invocation.Status != "PENDING" && invocation.Status != "NOTIFIED" {
			return nil, fmt.Errorf("invocation %s is not open for redelivery", invocationID)
		}
		runtimeState, exists := state.AgentRuntimes[runtimeID]
		if !exists || runtimeState.AgentID != invocation.Target {
			return nil, errors.New("redelivery runtime must be registered to the invocation target")
		}
		deliveryID := uuid.NewString()
		attemptEvent, err := s.Execute(actor, "invocation.delivery-attempt", invocationID,
			model.InvocationDeliveryAttempted{
				DeliveryID: deliveryID, RuntimeID: runtimeID,
				Transport: runtimeState.Connector, HostID: runtimeState.HostID, Manual: true,
			})
		if err != nil {
			return nil, err
		}
		updated, err := s.State()
		if err != nil {
			return nil, err
		}
		delivery := updated.InvocationDeliveries[deliveryID]
		if delivery.Status != "SUCCEEDED" {
			message := "redelivery did not complete"
			if delivery.Error != "" {
				message += ": " + delivery.Error
			}
			return nil, &controlplane.Error{Code: controlplane.CodeUnavailable, Message: message}
		}
		return map[string]any{"attempt_event": attemptEvent, "delivery": delivery}, nil
	case "invocation_policy_get":
		agentID := stringArg(p.Arguments, "agent_id")
		state, err := s.State()
		if err != nil {
			return nil, err
		}
		policy, configured := state.InvocationPolicies[agentID]
		return map[string]any{"configured": configured, "policy": policy}, nil
	case "invocation_policy_set":
		return s.Execute(actor, "invocation.policy.update", stringArg(p.Arguments, "agent_id"),
			model.InvocationPolicyUpdated{
				Mode:                          stringArg(p.Arguments, "mode"),
				TrustedActors:                 stringsArg(p.Arguments["trusted_actors"]),
				AllowedScopes:                 stringsArg(p.Arguments["allowed_scopes"]),
				DefaultConsumerMode:           model.ConsumerMode(stringArg(p.Arguments, "default_consumer_mode")),
				AllowedConsumerModes:          consumerModesArg(p.Arguments["allowed_consumer_modes"]),
				PreferredInteractiveRuntimeID: stringArg(p.Arguments, "preferred_interactive_runtime_id"),
				RequireHumanForSensitive:      boolArg(p.Arguments, "require_human_for_sensitive", false),
			})
	case "invocation_next":
		invocation, found, err := s.NextInvocation(actor, stringArg(p.Arguments, "runtime_id"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"found": found, "invocation": invocation}, nil
	case "invocation_listen":
		waitSeconds := intArg(p.Arguments, "wait_seconds")
		if waitSeconds == 0 {
			waitSeconds = int(controlplane.MaxInvocationListen / time.Second)
		}
		invocation, found, err := s.ListenInvocation(
			actor,
			stringArg(p.Arguments, "runtime_id"),
			time.Duration(waitSeconds)*time.Second,
		)
		if err != nil || !found {
			return map[string]any{"found": found, "invocation": invocation}, err
		}
		result := map[string]any{"found": true, "invocation": invocation, "claimed": false}
		if boolArg(p.Arguments, "auto_claim", true) {
			event, claimErr := s.Execute(actor, "invocation.claim", invocation.ID,
				model.InvocationClaimed{RuntimeID: stringArg(p.Arguments, "runtime_id")})
			if claimErr != nil {
				return nil, claimErr
			}
			result["claimed"] = true
			result["claim_event"] = event
		}
		return result, nil
	case "invocation_claim":
		return s.Execute(actor, "invocation.claim", stringArg(p.Arguments, "id"),
			model.InvocationClaimed{RuntimeID: stringArg(p.Arguments, "runtime_id")})
	case "invocation_start":
		return s.Execute(actor, "invocation.start", stringArg(p.Arguments, "id"),
			model.InvocationProgress{Summary: stringArg(p.Arguments, "summary")})
	case "invocation_wait":
		return s.Execute(actor, "invocation.wait", stringArg(p.Arguments, "id"),
			model.InvocationWaiting{Reason: stringArg(p.Arguments, "reason")})
	case "invocation_resume":
		return s.Execute(actor, "invocation.resume", stringArg(p.Arguments, "id"),
			model.InvocationProgress{Summary: stringArg(p.Arguments, "summary")})
	case "invocation_complete":
		return s.Execute(actor, "invocation.complete", stringArg(p.Arguments, "id"),
			model.InvocationCompleted{Summary: stringArg(p.Arguments, "summary"), ResultMessageID: stringArg(p.Arguments, "result_message_id")})
	case "invocation_reject":
		return s.Execute(actor, "invocation.reject", stringArg(p.Arguments, "id"),
			model.InvocationRejected{Reason: stringArg(p.Arguments, "reason")})
	case "runtime_register":
		kind := model.RuntimeKind(stringArg(p.Arguments, "kind"))
		if kind == "" {
			kind = model.RuntimeKindWorker
		}
		hostID, err := runtimeHostID(kind)
		if err != nil {
			return nil, err
		}
		return s.Execute(actor, "runtime.register", stringArg(p.Arguments, "id"), model.RuntimeRegistered{
			AgentID: actor, Kind: kind, Connector: stringArg(p.Arguments, "connector"),
			ConfigReference: stringArg(p.Arguments, "config_reference"), HostID: hostID,
			MaxConcurrent: intArg(p.Arguments, "max_concurrent"),
			Scopes:        stringsArg(p.Arguments["scopes"]), Capabilities: stringsArg(p.Arguments["capabilities"]),
		})
	case "runtime_configure":
		kind := model.RuntimeKind(stringArg(p.Arguments, "kind"))
		if kind == "" {
			kind = model.RuntimeKindWorker
		}
		hostID, err := runtimeHostID(kind)
		if err != nil {
			return nil, err
		}
		return s.Execute(actor, "runtime.configure", stringArg(p.Arguments, "id"), model.RuntimeConfigured{
			Kind: kind, Connector: stringArg(p.Arguments, "connector"),
			ConfigReference: stringArg(p.Arguments, "config_reference"), HostID: hostID,
			MaxConcurrent: intArg(p.Arguments, "max_concurrent"),
			Scopes:        stringsArg(p.Arguments["scopes"]), Capabilities: stringsArg(p.Arguments["capabilities"]),
		})
	case "runtime_heartbeat":
		return s.Execute(actor, "runtime.heartbeat", stringArg(p.Arguments, "id"), model.RuntimeHeartbeat{
			Health:            stringArg(p.Arguments, "health"),
			ActiveInvocations: stringsArg(p.Arguments["active_invocations"]),
			EndpointID:        stringArg(p.Arguments, "endpoint_id"),
		})
	}
	return nil, fmt.Errorf("unknown tool %s", p.Name)
}
func stringArg(m map[string]any, k string) string { v, _ := m[k].(string); return v }
func intArg(m map[string]any, key string) int {
	value, _ := m[key].(float64)
	return int(value)
}
func boolArg(values map[string]any, key string, fallback bool) bool {
	value, found := values[key]
	if !found {
		return fallback
	}
	result, ok := value.(bool)
	return ok && result
}
func stringsArg(v any) []string {
	a, _ := v.([]any)
	o := make([]string, 0, len(a))
	for _, x := range a {
		if s, ok := x.(string); ok {
			o = append(o, s)
		}
	}
	return o
}

func consumerModesArg(value any) []model.ConsumerMode {
	values := stringsArg(value)
	result := make([]model.ConsumerMode, len(values))
	for index, item := range values {
		result[index] = model.ConsumerMode(item)
	}
	return result
}

func runtimeHostID(kind model.RuntimeKind) (string, error) {
	if kind != model.RuntimeKindInteractive {
		return "", nil
	}
	return identity.LoadOrCreateHostID()
}
