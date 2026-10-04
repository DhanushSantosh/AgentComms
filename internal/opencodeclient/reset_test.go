package opencodeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOwnedInstanceResetRequiresCompletion(t *testing.T) {
	for _, outcome := range []string{"complete", "complete-no-space", "connected-only", "ack-only", "false", "empty", "foreign-directory", "foreign-property", "stream-error"} {
		t.Run(outcome, func(t *testing.T) {
			dispose := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/global/event":
					if outcome == "stream-error" {
						http.Error(w, "private provider text", 500)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					if outcome == "connected-only" {
						fmt.Fprint(w, "data:{\"payload\":{\"type\":\"server.connected\",\"properties\":{}}}\n\n")
					} else {
						fmt.Fprint(w, "data:{\"payload\":{\"type\":\"server.heartbeat\",\"properties\":{}}}\n\n")
					}
					w.(http.Flusher).Flush()
					select {
					case <-dispose:
					case <-r.Context().Done():
						return
					}
					if outcome != "ack-only" {
						directory, property := "/owned/project", "/owned/project"
						if outcome == "foreign-directory" {
							directory = "/foreign/project"
						}
						if outcome == "foreign-property" {
							property = "/foreign/project"
						}
						event, _ := json.Marshal(map[string]any{"directory": directory, "payload": map[string]any{"type": "server.instance.disposed", "properties": map[string]string{"directory": property}}})
						if outcome == "complete-no-space" {
							fmt.Fprintf(w, "data:%s\n\n", event)
						} else {
							fmt.Fprintf(w, "data: %s\n\n", event)
						}
						w.(http.Flusher).Flush()
					}
					<-r.Context().Done()
				case "/instance/dispose":
					if outcome == "false" {
						fmt.Fprint(w, "false")
					} else if outcome != "empty" {
						fmt.Fprint(w, "true")
					}
					select {
					case dispose <- struct{}{}:
					default:
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			err := New(server.URL, "/owned/project").DisposeInstance(ctx)
			if (err == nil) != (outcome == "complete" || outcome == "complete-no-space") {
				t.Fatalf("unproven reset outcome=%s error=%v", outcome, err)
			}
		})
	}
}
