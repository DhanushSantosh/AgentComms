package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/opencodeclient"
)

func TestOpenCodeLiveDoesNotApproveForeignSession(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		t.Run(fmt.Sprintf("resumed=%v", resumed), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			events := make(chan string, 2)
			ownedReplied, streamClosed := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var mu sync.Mutex
			foreignReply := ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// EnsureServer probes GET /session with an unscoped client.
				// Keep this healthy so the fixture never falls back to port 4096.
				if r.URL.Path == "/session" && r.Method == http.MethodGet {
					fmt.Fprint(w, `[]`)
					return
				}
				if r.Header.Get("x-opencode-directory") != url.PathEscape(root) {
					t.Errorf("wrong project routing on %s: %q", r.URL.Path, r.Header.Get("x-opencode-directory"))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				switch r.URL.Path {
				case "/session", "/session/ses-owned":
					json.NewEncoder(w).Encode(opencodeclient.Session{ID: "ses-owned", Directory: root})
				case "/event":
					defer close(streamClosed)
					w.Header().Set("Content-Type", "text/event-stream")
					w.(http.Flusher).Flush()
					for {
						select {
						case event := <-events:
							fmt.Fprintf(w, "data: %s\n\n", event)
							w.(http.Flusher).Flush()
						case <-r.Context().Done():
							return
						}
					}
				case "/session/ses-owned/message":
					events <- `{"type":"permission.asked","properties":{"id":"per-foreign","sessionID":"ses-foreign","permission":"edit"}}`
					events <- `{"type":"permission.asked","properties":{"id":"per-owned","sessionID":"ses-owned","permission":"read"}}`
					select {
					case <-ownedReplied:
						json.NewEncoder(w).Encode(opencodeclient.PromptResponse{Parts: []opencodeclient.Part{{Type: "text", Text: "synthetic receipt"}}})
					case <-r.Context().Done():
					}
				case "/permission/per-foreign/reply":
					var body struct {
						Reply string `json:"reply"`
					}
					json.NewDecoder(r.Body).Decode(&body)
					mu.Lock()
					foreignReply = body.Reply
					mu.Unlock()
					fmt.Fprint(w, `true`)
				case "/permission/per-owned/reply":
					once.Do(func() { close(ownedReplied) })
					fmt.Fprint(w, `true`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			path, err := opencodeclient.ServerInfoPath(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(opencodeclient.ServerInfo{BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := opencodeclient.New(server.URL, "").Health(ctx); err != nil {
				t.Fatalf("owned fixture must be healthy before adapter execution: %v", err)
			}
			config := Config{Actor: "synthetic-agent", RuntimeID: "owned-runtime", WorkDir: root, PermissionMode: "acceptEdits", Status: func(string) {}}
			if resumed {
				config.SessionID = "ses-owned"
			}
			output, err := (openCodeLiveAdapter{}).Execute(ctx, config, model.Invocation{ID: "synthetic-invocation", Instruction: "Synthetic receipt"})
			if err != nil {
				t.Fatal(err)
			}
			if output != "synthetic receipt" {
				t.Fatalf("output = %q", output)
			}
			mu.Lock()
			reply := foreignReply
			mu.Unlock()
			if reply != "" {
				t.Fatalf("owned runtime answered foreign permission: %q", reply)
			}
			select {
			case <-streamClosed:
			case <-ctx.Done():
				t.Fatal("adapter did not close owned subscription")
			}
		})
	}
}
