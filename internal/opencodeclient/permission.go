package opencodeclient

import (
	"context"
	"net/http"
)

// PermissionRequest mirrors OpenCode's PermissionV1.Request: one pending
// tool-call permission request, tagged with the permission category string
// ("edit", "bash", "webfetch", ...) — OpenCode's analogue of ACP's ToolKind.
type PermissionRequest struct {
	ID         string   `json:"id"`
	SessionID  string   `json:"sessionID"`
	Permission string   `json:"permission"`
	Patterns   []string `json:"patterns"`
}

// ListPermissions returns every pending permission request across all
// sessions on this server.
func (c *Client) ListPermissions(ctx context.Context) ([]PermissionRequest, error) {
	var requests []PermissionRequest
	if err := c.do(ctx, http.MethodGet, "/permission", nil, &requests); err != nil {
		return nil, err
	}
	return requests, nil
}

// ReplyPermission answers a pending permission request. reply must be one of
// "once", "always", or "reject" — OpenCode's PermissionV1.Reply.
func (c *Client) ReplyPermission(ctx context.Context, requestID, reply, message string) error {
	body := map[string]string{"reply": reply}
	if message != "" {
		body["message"] = message
	}
	return c.do(ctx, http.MethodPost, "/permission/"+requestID+"/reply", body, nil)
}

// Decision classifies how a permission category should be resolved, the
// OpenCode analogue of acpclient.Classify. Categories are OpenCode's own
// permission-config keys (confirmed against its schema), not ACP ToolKinds —
// the two packages intentionally don't share a type, since they classify two
// different providers' native vocabularies.
type Decision int

const (
	DecisionAutoApprove Decision = iota
	DecisionModeGated
	DecisionGoverned
)

// Classify maps an OpenCode permission category to a Decision. Unknown or
// future categories fall through to DecisionGoverned — consistent with
// acpclient.Classify, this errs toward asking rather than silently trusting
// an unrecognized category.
func Classify(permission string) Decision {
	switch permission {
	case "read", "glob", "grep", "list", "lsp":
		return DecisionAutoApprove
	case "edit":
		return DecisionModeGated
	default: // bash, webfetch, websearch, task, todowrite, skill, external_directory, question, doom_loop, ...
		return DecisionGoverned
	}
}
