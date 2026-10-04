package opencodeclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// PermissionRule preserves native order and wildcard semantics. It is not a
// second evaluator: the provider evaluates the restrictive ruleset itself.
type PermissionRule struct {
	Permission string `json:"permission"`
	Pattern    string `json:"pattern"`
	Action     string `json:"action"`
}

type Agent struct {
	Name       string           `json:"name"`
	Mode       string           `json:"mode"`
	Hidden     bool             `json:"hidden"`
	Native     *bool            `json:"native"`
	Permission []PermissionRule `json:"permission"`
}

// RestrictivePermissions changes only native allows into asks. A leading ask
// supplies the safe fallback; appending it would override native deny rules.
func RestrictivePermissions(agent, original []PermissionRule) ([]PermissionRule, error) {
	rules := []PermissionRule{{Permission: "*", Pattern: "*", Action: "ask"}}
	for _, source := range [][]PermissionRule{agent, original} {
		for _, rule := range source {
			if rule.Permission == "" || rule.Pattern == "" {
				return nil, errors.New("native permission rule lacks permission or pattern")
			}
			switch rule.Action {
			case "allow":
				rule.Action = "ask"
			case "ask", "deny":
			default:
				return nil, errors.New("unsupported native permission action")
			}
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

// ResolveAgent resolves an exact identity supplied by native preparation.
// It neither guesses a default from sorted lists nor reads resolved config.
func (c *Client) ResolveAgent(ctx context.Context, name string) (Agent, error) {
	if name == "" {
		return Agent{}, errors.New("native agent identity is required")
	}
	var agents []Agent
	if err := c.do(ctx, http.MethodGet, "/agent", nil, &agents); err != nil {
		return Agent{}, errors.New("native agent rules lookup failed")
	}
	var selected *Agent
	for _, agent := range agents {
		if agent.Name != name {
			continue
		}
		if selected != nil {
			return Agent{}, errors.New("native agent identity is ambiguous")
		}
		copy := agent
		selected = &copy
	}
	if selected == nil || selected.Hidden || (selected.Mode != "primary" && selected.Mode != "all") || selected.Permission == nil {
		return Agent{}, errors.New("native agent/rules unavailable or not a visible primary agent")
	}
	if _, err := RestrictivePermissions(selected.Permission, nil); err != nil {
		return Agent{}, err
	}
	return *selected, nil
}

// AppendSessionPermissions mirrors the native PATCH append contract. Callers
// must establish a fresh rule baseline and verify the complete readback.
func (c *Client) AppendSessionPermissions(ctx context.Context, id string, rules []PermissionRule) (Session, error) {
	if id == "" {
		return Session{}, errors.New("native session identity is required")
	}
	if _, err := RestrictivePermissions(nil, rules); err != nil {
		return Session{}, err
	}
	var session Session
	if err := c.do(ctx, http.MethodPatch, "/session/"+url.PathEscape(id), map[string]any{"permission": rules}, &session); err != nil {
		return Session{}, err
	}
	return session, nil
}
