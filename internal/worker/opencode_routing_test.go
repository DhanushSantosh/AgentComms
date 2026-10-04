package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
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
			disposed := make(chan struct{}, 1)
			ownedReplied, streamClosed := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var mu sync.Mutex
			foreignReply := ""
			var permissions []opencodeclient.PermissionRule
			markers := map[string]opencodeclient.PreparationMessage{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("x-opencode-directory") != url.PathEscape(root) {
					t.Errorf("wrong project routing on %s: %q", r.URL.Path, r.Header.Get("x-opencode-directory"))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				switch r.URL.Path {
				case "/global/event":
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data:{\"payload\":{\"type\":\"server.heartbeat\",\"properties\":{}}}\n\n")
					w.(http.Flusher).Flush()
					select {
					case <-disposed:
						payload, _ := json.Marshal(map[string]any{"directory": root, "payload": map[string]any{"type": "server.instance.disposed", "properties": map[string]string{"directory": root}}})
						fmt.Fprintf(w, "data: %s\n\n", payload)
						w.(http.Flusher).Flush()
					case <-r.Context().Done():
						return
					}
					<-r.Context().Done()
				case "/instance/dispose":
					fmt.Fprint(w, `true`)
					disposed <- struct{}{}
				case "/agent":
					fmt.Fprint(w, `[{"name":"build","mode":"primary","permission":[{"permission":"*","pattern":"*","action":"allow"}]}]`)
				case "/session", "/session/ses-owned":
					mu.Lock()
					defer mu.Unlock()
					if r.Method == http.MethodPatch {
						var patch struct {
							Permission []opencodeclient.PermissionRule `json:"permission"`
						}
						json.NewDecoder(r.Body).Decode(&patch)
						permissions = append(permissions, patch.Permission...)
					}
					json.NewEncoder(w).Encode(opencodeclient.Session{ID: "ses-owned", Directory: root, Permission: permissions})
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
					var request struct {
						NoReply   bool   `json:"noReply"`
						MessageID string `json:"messageID"`
						Agent     string `json:"agent"`
					}
					json.NewDecoder(r.Body).Decode(&request)
					if request.NoReply {
						marker := opencodeclient.PreparationMessage{ID: request.MessageID, SessionID: "ses-owned", Agent: "build", Role: "user"}
						mu.Lock()
						markers[marker.ID] = marker
						permissions = []opencodeclient.PermissionRule{{Permission: "*", Pattern: "*", Action: "deny"}}
						mu.Unlock()
						json.NewEncoder(w).Encode(map[string]any{"info": marker, "parts": []opencodeclient.Part{}})
						return
					}
					if request.Agent != "build" {
						t.Error("prompt did not pin prepared native agent")
					}
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
					if strings.HasPrefix(r.URL.Path, "/session/ses-owned/message/") {
						id := strings.TrimPrefix(r.URL.Path, "/session/ses-owned/message/")
						mu.Lock()
						defer mu.Unlock()
						marker, exists := markers[id]
						if !exists {
							http.NotFound(w, r)
							return
						}
						if r.Method == http.MethodDelete {
							delete(markers, id)
							fmt.Fprint(w, `true`)
							return
						}
						json.NewEncoder(w).Encode(map[string]any{"info": marker, "parts": []opencodeclient.Part{}})
						return
					}
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			config := Config{Actor: "synthetic-agent", RuntimeID: "owned-runtime", WorkDir: root, PermissionMode: "acceptEdits", Status: func(string) {}}
			if resumed {
				config.SessionID = "ses-owned"
			}
			adapter := &openCodeLiveAdapter{start: func(context.Context, string) (*ownedLiveServer, error) {
				lifetime, stop := context.WithCancel(context.Background())
				owned := &ownedLiveServer{baseURL: server.URL, cancel: stop, done: make(chan struct{})}
				go func() { <-lifetime.Done(); close(owned.done) }()
				return owned, nil
			}}
			defer adapter.Close()
			output, err := adapter.Execute(ctx, config, model.Invocation{ID: "synthetic-invocation", Instruction: "Synthetic receipt"})
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
