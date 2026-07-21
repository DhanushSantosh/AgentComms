package daemonclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

const maxResponseBytes = 8 * 1024 * 1024

type Client struct {
	http *http.Client
}

type Health struct {
	Status          string `json:"status"`
	RuntimeMode     string `json:"runtime_mode"`
	ProjectID       string `json:"project_id"`
	ProtocolVersion int    `json:"protocol_version"`
}

func New(endpoint string, timeout time.Duration) (*Client, error) {
	if endpoint == "" {
		return nil, errors.New("daemon endpoint is required")
	}
	if timeout <= 0 {
		timeout = controlplane.DefaultRequestTimeout
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialLocal(ctx, endpoint)
		},
		MaxIdleConns: 32, MaxIdleConnsPerHost: 32, IdleConnTimeout: time.Minute,
	}
	return &Client{http: &http.Client{Transport: transport, Timeout: timeout}}, nil
}

func (c *Client) State(ctx context.Context, projectID string) (model.State, controlplane.ResultMetadata, error) {
	var response struct {
		State    model.State                 `json:"state"`
		Metadata controlplane.ResultMetadata `json:"metadata"`
	}
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/projects/%s/state", url.PathEscape(projectID)), nil, &response)
	return response.State, response.Metadata, err
}

func (c *Client) Command(ctx context.Context, command controlplane.Command) (controlplane.Event, controlplane.ResultMetadata, error) {
	var response struct {
		Event    controlplane.Event          `json:"event"`
		Metadata controlplane.ResultMetadata `json:"metadata"`
	}
	err := c.do(ctx, http.MethodPost, fmt.Sprintf("/v1/projects/%s/commands", url.PathEscape(command.ProjectID)), command, &response)
	return response.Event, response.Metadata, err
}

func (c *Client) Events(ctx context.Context, projectID string, page controlplane.PageRequest) (controlplane.EventPage, error) {
	query := url.Values{}
	if page.Cursor != "" {
		query.Set("cursor", page.Cursor)
	}
	if page.Limit != 0 {
		query.Set("limit", strconv.Itoa(page.Limit))
	}
	path := fmt.Sprintf("/v1/projects/%s/events", url.PathEscape(projectID))
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var response controlplane.EventPage
	err := c.do(ctx, http.MethodGet, path, nil, &response)
	return response, err
}

func (c *Client) NextInvocation(
	ctx context.Context,
	projectID string,
	actor string,
	runtimeID string,
	wait time.Duration,
) (model.Invocation, bool, controlplane.ResultMetadata, error) {
	if wait < 0 || wait > controlplane.MaxInvocationListen {
		return model.Invocation{}, false, controlplane.ResultMetadata{},
			fmt.Errorf("invocation listen duration must be from 0 to %s", controlplane.MaxInvocationListen)
	}
	query := url.Values{"actor": []string{actor}}
	if runtimeID != "" {
		query.Set("runtime", runtimeID)
	}
	query.Set("wait_ms", strconv.FormatInt(wait.Milliseconds(), 10))
	path := fmt.Sprintf("/v1/projects/%s/invocations/next?%s", url.PathEscape(projectID), query.Encode())
	var response struct {
		Found      bool                        `json:"found"`
		Invocation model.Invocation            `json:"invocation"`
		Metadata   controlplane.ResultMetadata `json:"metadata"`
	}
	err := c.do(ctx, http.MethodGet, path, nil, &response)
	return response.Invocation, response.Found, response.Metadata, err
}

func (c *Client) Verify(ctx context.Context, projectID string, from, to uint64) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/v1/projects/%s/verify", url.PathEscape(projectID)),
		map[string]uint64{"from": from, "to": to}, &map[string]any{})
}

func (c *Client) Healthy(ctx context.Context) error {
	_, err := c.Health(ctx)
	return err
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var health Health
	err := c.do(ctx, http.MethodGet, "/health/live", nil, &health)
	return health, err
}

func (c *Client) Shutdown(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/admin/shutdown", map[string]any{}, &map[string]any{})
}

func (c *Client) Sync(ctx context.Context, projectID string) (controlplane.ResultMetadata, error) {
	var response struct {
		Metadata controlplane.ResultMetadata `json:"metadata"`
	}
	err := c.do(ctx, http.MethodPost, fmt.Sprintf("/v1/projects/%s/sync", url.PathEscape(projectID)),
		map[string]any{}, &response)
	return response.Metadata, err
}

func (c *Client) SaveDraft(ctx context.Context, draft controlplane.Draft) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/v1/projects/%s/drafts", url.PathEscape(draft.ProjectID)),
		draft, &map[string]any{})
}

func (c *Client) Drafts(ctx context.Context, projectID string, limit int) ([]controlplane.Draft, error) {
	var response struct {
		Drafts []controlplane.Draft `json:"drafts"`
	}
	path := fmt.Sprintf("/v1/projects/%s/drafts?limit=%d", url.PathEscape(projectID), limit)
	err := c.do(ctx, http.MethodGet, path, nil, &response)
	return response.Drafts, err
}

func (c *Client) do(ctx context.Context, method, path string, requestBody, responseBody any) error {
	var body io.Reader
	if requestBody != nil {
		raw, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://daemon"+path, body)
	if err != nil {
		return err
	}
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return &controlplane.Error{Code: controlplane.CodeOffline, Message: "local daemon is unavailable: " + err.Error()}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxResponseBytes {
		return &controlplane.Error{Code: controlplane.CodeUnavailable, Message: "daemon response exceeds the configured limit"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error struct {
				Code         controlplane.ErrorCode `json:"code"`
				Message      string                 `json:"message"`
				RetryAfterMS int64                  `json:"retry_after_ms"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &failure) == nil && failure.Error.Code != "" {
			return &controlplane.Error{
				Code: failure.Error.Code, Message: failure.Error.Message,
				RetryAfter: time.Duration(failure.Error.RetryAfterMS) * time.Millisecond,
			}
		}
		return &controlplane.Error{Code: controlplane.CodeUnavailable, Message: response.Status}
	}
	if responseBody == nil {
		return nil
	}
	return json.Unmarshal(raw, responseBody)
}
