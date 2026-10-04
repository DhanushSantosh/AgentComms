package codexserve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/brokeridentity"
)

// Client talks to one local Codex live broker.
type Client struct {
	baseURL   string
	http      *http.Client
	projectID string
}

func New(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{}}
}

// NewForProject routes every runtime operation through the stored project
// identity. It never falls back to the legacy literal-ID namespace.
func NewForProject(baseURL, projectID string) (*Client, error) {
	if _, err := brokeridentity.RuntimeKey(projectID, "validation"); err != nil {
		return nil, err
	}
	client := New(baseURL)
	client.projectID = projectID
	return client, nil
}

func (c *Client) runtimePath(runtimeID string) (string, error) {
	if c.projectID != "" {
		key, err := brokeridentity.RuntimeKey(c.projectID, runtimeID)
		if err != nil {
			return "", err
		}
		return runtimePath(key), nil
	}
	return runtimePath(runtimeID), nil
}

func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/health", nil, nil)
}

// Register creates or reuses the persistent process for runtimeID and
// returns the thread ID it's actually bound to -- Codex mints its own
// thread IDs, so callers that need to persist one for later invocations
// must capture it from here rather than assuming the one they requested.
func (c *Client) Register(ctx context.Context, runtimeID string, config ProcessConfig) (string, error) {
	path, err := c.runtimePath(runtimeID)
	if err != nil {
		return "", err
	}
	var response struct {
		ThreadID string `json:"thread_id"`
	}
	if err := c.do(ctx, http.MethodPost, path+"/register", config, &response); err != nil {
		return "", err
	}
	return response.ThreadID, nil
}

func (c *Client) Prompt(ctx context.Context, runtimeID, text string) (string, error) {
	path, err := c.runtimePath(runtimeID)
	if err != nil {
		return "", err
	}
	var response struct {
		Output string `json:"output"`
	}
	if err := c.do(ctx, http.MethodPost, path+"/prompt", map[string]string{"text": text}, &response); err != nil {
		return "", err
	}
	return response.Output, nil
}

func runtimePath(runtimeID string) string {
	return "/runtimes/" + url.PathEscape(runtimeID)
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
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("codexserve: %s %s: %w", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxStreamLineBytes))
	if err != nil {
		return err
	}
	if response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("codexserve: %s %s: status %d: %s", method, path, response.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Subscribe opens the read-only SSE stream for one runtime.
func (c *Client) Subscribe(ctx context.Context, runtimeID string) (<-chan []byte, error) {
	path, err := c.runtimePath(runtimeID)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"/events", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("codexserve: subscribe: %w", err)
	}
	if response.StatusCode >= http.StatusMultipleChoices {
		defer func() { _ = response.Body.Close() }()
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("codexserve: subscribe: status %d: %s", response.StatusCode, strings.TrimSpace(string(raw)))
	}
	events := make(chan []byte)
	go func() {
		defer close(events)
		defer func() { _ = response.Body.Close() }()
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), maxStreamLineBytes)
		for scanner.Scan() {
			data, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok {
				continue
			}
			select {
			case events <- []byte(data):
			case <-ctx.Done():
				return
			}
		}
	}()
	return events, nil
}
