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
	baseURL       string
	http          *http.Client
	authorization string
}

func NewMigration(baseURL, token string, timeout time.Duration) (*Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("migration token is required")
	}
	client, err := New(baseURL, timeout)
	if err != nil {
		return nil, err
	}
	client.authorization = "Bearer " + token
	return client, nil
}

func New(baseURL string, timeout time.Duration) (*Client, error) {
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

func (c *Client) CreateProject(ctx context.Context, projectID, ownerID string) error {
	return c.doJSON(ctx, http.MethodPost, "/v1/projects",
		map[string]string{"project_id": projectID, "owner_id": ownerID}, &map[string]any{})
}

func (c *Client) BeginLegacyImport(ctx context.Context, projectID string, request any, response any) error {
	return c.doJSON(ctx, http.MethodPost,
		fmt.Sprintf("/v1/projects/%s/imports/legacy", url.PathEscape(projectID)), request, response)
}

func (c *Client) ImportLegacyBatch(ctx context.Context, projectID string, request any, response any) error {
	return c.doJSON(ctx, http.MethodPost,
		fmt.Sprintf("/v1/projects/%s/imports/legacy/batches", url.PathEscape(projectID)), request, response)
}

func (c *Client) FinalizeLegacyImport(ctx context.Context, projectID string, request any, response any) error {
	return c.doJSON(ctx, http.MethodPost,
		fmt.Sprintf("/v1/projects/%s/imports/legacy/finalize", url.PathEscape(projectID)), request, response)
}

func (c *Client) LegacyImportStatus(ctx context.Context, projectID string, response any) error {
	return c.doJSON(ctx, http.MethodGet,
		fmt.Sprintf("/v1/projects/%s/imports/legacy", url.PathEscape(projectID)), nil, response)
}

func (c *Client) BeginAttestedImport(ctx context.Context, projectID string, request any, response any) error {
	return c.doJSON(ctx, http.MethodPost,
		fmt.Sprintf("/v1/projects/%s/imports/attested", url.PathEscape(projectID)), request, response)
}

func (c *Client) ImportAttestedBatch(ctx context.Context, projectID string, request any, response any) error {
	return c.doJSON(ctx, http.MethodPost,
		fmt.Sprintf("/v1/projects/%s/imports/attested/batches", url.PathEscape(projectID)), request, response)
}

func (c *Client) FinalizeAttestedImport(ctx context.Context, projectID string, request any, response any) error {
	return c.doJSON(ctx, http.MethodPost,
		fmt.Sprintf("/v1/projects/%s/imports/attested/finalize", url.PathEscape(projectID)), request, response)
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
	if c.authorization != "" {
		request.Header.Set("Authorization", c.authorization)
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
