package opencodeclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Event is one message from the server's /event SSE stream. Properties is
// left undecoded — callers decode only the event types they act on.
type Event struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

// PermissionAskedProperties decodes the Properties of a "permission.asked"
// event into the same PermissionRequest shape ListPermissions returns.
type PermissionAskedProperties = PermissionRequest

func decodeInto(raw json.RawMessage, out any) error {
	return json.Unmarshal(raw, out)
}

// Subscribe opens the server's /event SSE stream and sends every event it
// receives on the returned channel. The channel is closed when ctx is
// cancelled or the connection ends; callers must drain it to avoid leaking
// the underlying goroutine.
func Subscribe(ctx context.Context, client *Client) (<-chan Event, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+"/event", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencodeclient: subscribe: %w", err)
	}
	if resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("opencodeclient: subscribe: status %d", resp.StatusCode)
	}

	events := make(chan Event)
	go func() {
		defer close(events)
		defer func() { _ = resp.Body.Close() }()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			data, ok := strings.CutPrefix(line, "data: ")
			if !ok {
				continue
			}
			var event Event
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return events, nil
}
