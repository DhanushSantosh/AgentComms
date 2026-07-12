package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

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
		tool("history", "Read immutable signed events", map[string]any{}),
		tool("task_create", "Create a coordination task", map[string]any{"id": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "repository": map[string]any{"type": "string"}, "branch": map[string]any{"type": "string"}, "resources": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "id", "title", "repository", "branch", "resources"),
		tool("task_claim", "Claim an open task with a protected lease", map[string]any{"id": map[string]any{"type": "string"}}, "id"),
		tool("message_post", "Post a typed durable message", map[string]any{"id": map[string]any{"type": "string"}, "kind": map[string]any{"type": "string", "enum": []string{"FYI", "ACTION", "CONTRACT", "BLOCKER", "DECISION"}}, "to": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "subject": map[string]any{"type": "string"}, "body": map[string]any{"type": "string"}}, "id", "kind", "to", "subject"),
		tool("verify", "Verify signatures and hash-chain integrity", map[string]any{}),
	}
}
func Serve(s *service.Service, actor string, in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
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
		return s.Store.Events()
	case "verify":
		if e := s.Store.Verify(); e != nil {
			return nil, e
		}
		return map[string]any{"verified": true, "head": s.Store.Head()}, nil
	case "task_create":
		id, _ := p.Arguments["id"].(string)
		res := stringsArg(p.Arguments["resources"])
		return s.Execute(actor, "task.create", id, model.TaskCreated{Title: stringArg(p.Arguments, "title"), Repository: stringArg(p.Arguments, "repository"), Branch: stringArg(p.Arguments, "branch"), Resources: res})
	case "task_claim":
		return s.Execute(actor, "task.claim", stringArg(p.Arguments, "id"), model.TaskClaimed{})
	case "message_post":
		return s.Execute(actor, "message.post", stringArg(p.Arguments, "id"), model.MessagePosted{Kind: stringArg(p.Arguments, "kind"), To: stringsArg(p.Arguments["to"]), Subject: stringArg(p.Arguments, "subject"), Body: stringArg(p.Arguments, "body")})
	}
	return nil, fmt.Errorf("unknown tool %s", p.Name)
}
func stringArg(m map[string]any, k string) string { v, _ := m[k].(string); return v }
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
