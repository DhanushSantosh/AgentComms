package opencodeclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// DisposeInstance must be called only on a server owned by this runtime.
// Native HTTP acknowledgment precedes teardown and can conceal disposer
// failures. The exact project's global completion event is emitted only after
// the native disposers have completed; subscribe before requesting disposal.
func (c *Client) DisposeInstance(ctx context.Context) error {
	if c.directory == "" {
		return errors.New("owned native project directory is required for reset")
	}
	resetCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(resetCtx, http.MethodGet, c.baseURL+"/global/event", nil)
	if err != nil {
		return errors.New("native reset completion stream unavailable")
	}
	request.Header.Set("Accept", "text/event-stream")
	c.setDirectoryHeader(request)
	response, err := c.http.Do(request)
	if err != nil {
		return errors.New("native reset completion stream unavailable")
	}
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		response.Body.Close()
		return errors.New("native reset completion stream unavailable")
	}
	completion := make(chan error, 1)
	ready := make(chan struct{}, 1)
	requested := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			data, ok := strings.CutPrefix(scanner.Text(), "data:")
			if !ok {
				continue
			}
			data = strings.TrimPrefix(data, " ")
			var envelope struct {
				Directory string `json:"directory"`
				Payload   struct {
					Type       string `json:"type"`
					Properties struct {
						Directory string `json:"directory"`
					} `json:"properties"`
				} `json:"payload"`
			}
			if json.Unmarshal([]byte(data), &envelope) != nil {
				continue
			}
			// Native server.connected is emitted before the global bus listener
			// is acquired. The merged heartbeat proves the event stream has
			// progressed past that initial frame; headers alone are not ready.
			if envelope.Payload.Type == "server.heartbeat" {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
			if envelope.Directory == c.directory && envelope.Payload.Type == "server.instance.disposed" && envelope.Payload.Properties.Directory == c.directory {
				select {
				case <-requested:
					completion <- nil
					return
				default:
				}
			}
		}
		completion <- errors.New("native reset completion stream ended without proof")
	}()
	defer func() {
		cancel()
		response.Body.Close()
		<-done
	}()
	select {
	case <-ready:
	case err := <-completion:
		return err
	case <-resetCtx.Done():
		return errors.New("native reset completion stream readiness was not confirmed")
	}
	close(requested)
	var accepted bool
	if err := c.do(resetCtx, http.MethodPost, "/instance/dispose", nil, &accepted); err != nil || !accepted {
		return errors.New("native instance reset was not acknowledged")
	}
	select {
	case err := <-completion:
		return err
	case <-resetCtx.Done():
		return errors.New("native instance reset completion was not confirmed")
	}
}
