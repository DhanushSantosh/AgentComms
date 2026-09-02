package remote

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
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
)

const maxResponseBytes = 8 * 1024 * 1024

type Client struct {
	baseURL string
	http    *http.Client
	token   string
}

func New(baseURL string, timeout time.Duration) (*Client, error) {
	return NewWithToken(baseURL, timeout, "")
}

func NewWithToken(baseURL string, timeout time.Duration, token string) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("authority URL must be an absolute HTTP or HTTPS URL")
	}
	if timeout <= 0 {
		timeout = controlplane.DefaultRequestTimeout
	}
	transport := &http.Transport{
		Proxy:        http.ProxyFromEnvironment,
		DialContext:  (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns: 100, MaxIdleConnsPerHost: 32, IdleConnTimeout: 90 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: timeout,
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Transport: transport, Timeout: timeout},
		token:   strings.TrimSpace(token),
	}, nil
}

func (c *Client) Command(ctx context.Context, command controlplane.Command) (controlplane.Event, controlplane.Receipt, error) {
	var response struct {
		Event    controlplane.Event `json:"event"`
		Metadata struct {
			Receipt controlplane.Receipt `json:"receipt"`
		} `json:"metadata"`
	}
	err := c.doJSON(ctx, http.MethodPost,
		fmt.Sprintf("/v1/projects/%s/commands", url.PathEscape(command.ProjectID)), command, &response)
	return response.Event, response.Metadata.Receipt, err
}

func (c *Client) Events(ctx context.Context, projectID string, page controlplane.PageRequest) (controlplane.EventPage, error) {
	query := url.Values{}
	if page.Cursor != "" {
		query.Set("cursor", page.Cursor)
	}
	if page.Limit != 0 {
		query.Set("limit", strconv.Itoa(page.Limit))
	}
	var response controlplane.EventPage
	path := fmt.Sprintf("/v1/projects/%s/events", url.PathEscape(projectID))
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	err := c.doJSON(ctx, http.MethodGet, path, nil, &response)
	return response, err
}

func (c *Client) Healthy(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodGet, "/health/ready", nil, &map[string]any{})
}

// Capabilities returns the authority's raw /v1/capabilities response
// (product_version, authority_api_version, schema_version, ...). Used by
// DeleteProject (RFC 0020) to detect an authority server too old to
// support project deletion before attempting one, for a clear
// "upgrade the authority server" error instead of a raw 404.
func (c *Client) Capabilities(ctx context.Context) (map[string]any, error) {
	var response map[string]any
	err := c.doJSON(ctx, http.MethodGet, "/v1/capabilities", nil, &response)
	return response, err
}

func (c *Client) CreateProject(ctx context.Context, projectID, ownerID string) error {
	return c.doJSON(ctx, http.MethodPost, "/v1/projects",
		map[string]string{"project_id": projectID, "owner_id": ownerID}, &map[string]any{})
}

// DeleteProject permanently deletes projectID and every row scoped to it
// from the authority -- see RFC 0020. command must be signed with the
// OWNER actor's elevated key; authorization is verified server-side, not
// trusted from the caller. Idempotent: deleting an already-deleted or
// never-existing project returns a CodeValidation error, not a panic or an
// ambiguous success -- safe to retry after a dropped response.
func (c *Client) DeleteProject(ctx context.Context, command controlplane.Command) error {
	return c.doJSON(ctx, http.MethodDelete,
		fmt.Sprintf("/v1/projects/%s", url.PathEscape(command.ProjectID)), command, &map[string]any{})
}

func (c *Client) doJSON(ctx context.Context, method, path string, requestBody, responseBody any) error {
	var body io.Reader
	if requestBody != nil {
		raw, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return &controlplane.Error{Code: controlplane.CodeOffline, Message: "authority is unreachable: " + err.Error()}
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return &controlplane.Error{Code: controlplane.CodeUnavailable, Message: err.Error()}
	}
	if len(raw) > maxResponseBytes {
		return &controlplane.Error{Code: controlplane.CodeUnavailable, Message: "authority response exceeds the configured limit"}
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
		return &controlplane.Error{Code: controlplane.CodeUnavailable, Message: fmt.Sprintf("authority returned %s", response.Status)}
	}
	if responseBody == nil || len(raw) == 0 {
		return nil
	}
	if err = json.Unmarshal(raw, responseBody); err != nil {
		return &controlplane.Error{Code: controlplane.CodeUnavailable, Message: "decode authority response: " + err.Error()}
	}
	return nil
}
