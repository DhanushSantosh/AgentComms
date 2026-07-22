// Package opencodeclient is a Go client for OpenCode's native REST + SSE
// server API (`opencode serve`) — a provider-specific alternative to driving
// OpenCode over ACP. It exists for exactly one reason ACP cannot deliver:
// OpenCode's own web UI, pointed at the same running server, shows a live,
// natively-rendered view of a session as it happens. Confirmed live that
// ACP-driven sessions never produce this — a separately-spawned `opencode
// acp` subprocess's activity never reaches a running server's SSE stream,
// even though the same on-disk session store makes it readable after the
// fact via REST polling. Driving the session through the server this
// package talks to is what makes the live view real, not a workaround.
package opencodeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client is a thin wrapper around one opencode serve instance's REST API.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client for the server at baseURL (e.g. "http://127.0.0.1:4096").
func New(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{}}
}

// BaseURL returns the server address this client talks to — the same
// address a user opens in a browser to watch a session live.
func (c *Client) BaseURL() string { return c.baseURL }

// Session is the subset of OpenCode's session.Info this package uses.
type Session struct {
	ID        string `json:"id"`
	Directory string `json:"directory"`
	Title     string `json:"title"`
}

// CreateSession creates a new session rooted at directory.
func (c *Client) CreateSession(ctx context.Context, directory string) (Session, error) {
	var session Session
	body := map[string]string{"directory": directory}
	if err := c.do(ctx, http.MethodPost, "/session", body, &session); err != nil {
		return Session{}, fmt.Errorf("opencodeclient: create session: %w", err)
	}
	return session, nil
}

// GetSession fetches a session by ID, confirming it exists before resuming it.
func (c *Client) GetSession(ctx context.Context, id string) (Session, error) {
	var session Session
	if err := c.do(ctx, http.MethodGet, "/session/"+id, nil, &session); err != nil {
		return Session{}, fmt.Errorf("opencodeclient: get session %s: %w", id, err)
	}
	return session, nil
}

// TextPart is a plain-text prompt part.
type TextPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// NewTextPart builds a single text-only prompt part.
func NewTextPart(text string) TextPart { return TextPart{Type: "text", Text: text} }

// PromptRequest is the payload for sending one prompt turn.
type PromptRequest struct {
	Parts []TextPart `json:"parts"`
	// System, if set, is delivered as this turn's system prompt — the
	// OpenCode analogue of Claude's `_meta.systemPrompt.append` extension,
	// used to carry the runtime operating-convention framing on a channel
	// separate from the task instruction itself.
	System string `json:"system,omitempty"`
}

// Part is one part of a response message (text content is what this package
// currently reads; other part types are decoded but ignored).
type Part struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// PromptResponse is the assistant's reply to one prompt turn.
type PromptResponse struct {
	Parts []Part `json:"parts"`
}

// Text concatenates every text part of the response, in order.
func (r PromptResponse) Text() string {
	var b strings.Builder
	for _, part := range r.Parts {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// Prompt sends one prompt turn and blocks until the assistant's turn
// completes — matching the exec- and ACP-based adapters' synchronous
// execution model. Tool-call permission requests raised mid-turn are not
// handled here; call StartPermissionWatcher concurrently before Prompt so
// they get answered while this call is in flight.
func (c *Client) Prompt(ctx context.Context, sessionID string, req PromptRequest) (PromptResponse, error) {
	var resp PromptResponse
	if err := c.do(ctx, http.MethodPost, "/session/"+sessionID+"/message", req, &resp); err != nil {
		return PromptResponse{}, fmt.Errorf("opencodeclient: prompt: %w", err)
	}
	return resp, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Health reports whether the server at baseURL is reachable at all —
// callers use this to decide whether a persistent server needs spawning.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/session", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return errors.New("server unhealthy")
	}
	return nil
}
