package claudeserve

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
)

// Client talks to one local Claude live broker.
type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{}}
}

func (c *Client) BaseURL() string { return c.baseURL }

func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/health", nil, nil)
}

func (c *Client) Register(ctx context.Context, runtimeID string, config ProcessConfig) error {
	return c.do(ctx, http.MethodPost, runtimePath(runtimeID)+"/register", config, nil)
}

func (c *Client) Prompt(ctx context.Context, runtimeID, text string) (string, error) {
	var response struct {
		Output string `json:"output"`
	}
	if err := c.do(ctx, http.MethodPost, runtimePath(runtimeID)+"/prompt", map[string]string{"text": text}, &response); err != nil {
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
		return fmt.Errorf("claudeserve: %s %s: %w", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxStreamLineBytes))
	if err != nil {
		return err
	}
	if response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("claudeserve: %s %s: status %d: %s", method, path, response.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Subscribe opens the read-only SSE stream for one runtime.
func (c *Client) Subscribe(ctx context.Context, runtimeID string) (<-chan []byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+runtimePath(runtimeID)+"/events", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("claudeserve: subscribe: %w", err)
	}
	if response.StatusCode >= http.StatusMultipleChoices {
		defer func() { _ = response.Body.Close() }()
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("claudeserve: subscribe: status %d: %s", response.StatusCode, strings.TrimSpace(string(raw)))
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
