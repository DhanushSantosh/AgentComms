package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
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

func tool(name, description string, properties map[string]any, required ...string) map[string]any {
	return map[string]any{"name": name, "description": description, "inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}
}
func tools() []map[string]any {
	return []map[string]any{
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
			"task_id":  map[string]any{"type": "string"},
			"scopes":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"priority": map[string]any{"type": "string", "enum": []string{"LOW", "NORMAL", "HIGH", "URGENT"}},
		}, "id", "target", "instruction"),
		tool("invocation_next", "Read the next claimable invocation for this agent", map[string]any{
			"runtime_id": map[string]any{"type": "string"},
		}),
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
		tool("runtime_register", "Register an agent runtime without embedding connector secrets", map[string]any{
			"id": map[string]any{"type": "string"}, "connector": map[string]any{"type": "string"},
			"config_reference": map[string]any{"type": "string"}, "max_concurrent": map[string]any{"type": "integer", "minimum": 1, "maximum": controlplane.MaxRuntimeConcurrency},
			"scopes":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"capabilities": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, "id", "connector", "max_concurrent"),
		tool("runtime_heartbeat", "Update this agent runtime's bounded presence record", map[string]any{
			"id": map[string]any{"type": "string"}, "health": map[string]any{"type": "string", "enum": []string{"HEALTHY", "DEGRADED"}},
			"active_invocations": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, "id", "health"),
		tool("verify", "Verify signatures and hash-chain integrity", map[string]any{}),
	}
}
func Serve(s *service.Service, actor string, in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 64*1024), controlplane.MaxCommandBytes)
	enc := json.NewEncoder(out)
	for scan.Scan() {
		var q request
		if e := json.Unmarshal(scan.Bytes(), &q); e != nil {
			_ = enc.Encode(response{JSONRPC: "2.0", ID: nil, Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		r := handle(s, actor, q)
		if e := enc.Encode(r); e != nil {
			return e
		}
	}
	return scan.Err()
}
func handle(s *service.Service, actor string, q request) response {
	r := response{JSONRPC: "2.0", ID: q.ID}
	switch q.Method {
	case "initialize":
		r.Result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "agent-comms", "version": "0.2.0-preview.1"}}
	case "notifications/initialized":
		r.Result = map[string]any{}
	case "tools/list":
		r.Result = map[string]any{"tools": tools()}
	case "tools/call":
		var p callParams
		if e := json.Unmarshal(q.Params, &p); e != nil {
			return rpcFail(r, -32602, e)
		}
		v, e := call(s, actor, p)
		if e != nil {
			return rpcFail(r, -32000, e)
		}
		b, _ := json.Marshal(v)
		r.Result = map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}, "structuredContent": v}
	default:
		return rpcFail(r, -32601, fmt.Errorf("method %s not found", q.Method))
	}
	return r
}
func rpcFail(r response, code int, e error) response {
	r.Error = &rpcError{Code: code, Message: e.Error()}
	return r
}
func call(s *service.Service, actor string, p callParams) (any, error) {
	switch p.Name {
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
	case "task_create":
		id, _ := p.Arguments["id"].(string)
		res := stringsArg(p.Arguments["resources"])
		return s.Execute(actor, "task.create", id, model.TaskCreated{Title: stringArg(p.Arguments, "title"), Repository: stringArg(p.Arguments, "repository"), Branch: stringArg(p.Arguments, "branch"), Resources: res})
	case "task_claim":
		return s.Execute(actor, "task.claim", stringArg(p.Arguments, "id"), model.TaskClaimed{})
	case "message_post":
		return s.Execute(actor, "message.post", stringArg(p.Arguments, "id"), model.MessagePosted{Kind: stringArg(p.Arguments, "kind"), To: stringsArg(p.Arguments["to"]), Subject: stringArg(p.Arguments, "subject"), Body: stringArg(p.Arguments, "body")})
	case "invocation_request":
		return s.Execute(actor, "invocation.request", stringArg(p.Arguments, "id"), model.InvocationRequested{
			Target: stringArg(p.Arguments, "target"), MessageID: stringArg(p.Arguments, "message_id"),
			TaskID: stringArg(p.Arguments, "task_id"), Instruction: stringArg(p.Arguments, "instruction"),
			ExpectedResult: stringArg(p.Arguments, "expected_result"),
			Scopes:         stringsArg(p.Arguments["scopes"]), Priority: stringArg(p.Arguments, "priority"),
		})
	case "invocation_next":
		invocation, found, err := s.NextInvocation(actor, stringArg(p.Arguments, "runtime_id"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"found": found, "invocation": invocation}, nil
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
		return s.Execute(actor, "runtime.register", stringArg(p.Arguments, "id"), model.RuntimeRegistered{
			AgentID: actor, Connector: stringArg(p.Arguments, "connector"),
			ConfigReference: stringArg(p.Arguments, "config_reference"),
			MaxConcurrent:   intArg(p.Arguments, "max_concurrent"),
			Scopes:          stringsArg(p.Arguments["scopes"]), Capabilities: stringsArg(p.Arguments["capabilities"]),
		})
	case "runtime_heartbeat":
		return s.Execute(actor, "runtime.heartbeat", stringArg(p.Arguments, "id"), model.RuntimeHeartbeat{
			Health:            stringArg(p.Arguments, "health"),
			ActiveInvocations: stringsArg(p.Arguments["active_invocations"]),
		})
	}
	return nil, fmt.Errorf("unknown tool %s", p.Name)
}
func stringArg(m map[string]any, k string) string { v, _ := m[k].(string); return v }
func intArg(m map[string]any, key string) int {
	value, _ := m[key].(float64)
	return int(value)
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
