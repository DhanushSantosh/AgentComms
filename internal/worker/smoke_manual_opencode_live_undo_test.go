package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/opencodeclient"
)

func TestManualSmokeOpenCodeLivePreservesPendingUndo(t *testing.T) {
	if os.Getenv("AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE=1 for actual native undo preservation verification")
	}
	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	adapter := &openCodeLiveAdapter{}
	defer adapter.Close()
	config := Config{Actor: "synthetic", RuntimeID: "undo-owned", WorkDir: root, PermissionMode: "plan"}
	if _, err := adapter.Execute(ctx, config, model.Invocation{ID: "undo-history", Instruction: "Reply exactly NATIVE-UNDO-HISTORY-PROOF. Do not use tools."}); err != nil {
		t.Fatal(err)
	}
	id := adapter.sessionID
	request := func(endpoint, method, path string, body any, out any) {
		t.Helper()
		var encoded []byte
		if body != nil {
			var err error
			encoded, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint+path, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("x-opencode-directory", url.PathEscape(adapter.workDir))
		req.Header.Set("Content-Type", "application/json")
		response, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("native undo fixture status=%d", response.StatusCode)
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	messagePath := "/session/" + url.PathEscape(id) + "/message"
	var history []struct {
		Info struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"info"`
	}
	request(adapter.server.baseURL, http.MethodGet, messagePath, nil, &history)
	userID := ""
	for _, message := range history {
		if message.Info.Role == "user" {
			userID = message.Info.ID
		}
	}
	if userID == "" {
		t.Fatal("native history did not contain a user message")
	}
	var reverted opencodeclient.Session
	request(adapter.server.baseURL, http.MethodPost, "/session/"+url.PathEscape(id)+"/revert", map[string]string{"messageID": userID}, &reverted)
	if len(reverted.Revert) == 0 || bytes.Equal(bytes.TrimSpace(reverted.Revert), []byte("null")) {
		t.Fatal("native Undo state was not established")
	}
	var before any
	request(adapter.server.baseURL, http.MethodGet, messagePath, nil, &before)
	_, err := adapter.Execute(ctx, config, model.Invocation{ID: "must-not-truncate", Instruction: "Reply exactly MUST-NOT-RUN. Do not use tools."})
	if err == nil || !strings.Contains(err.Error(), "pending undo/revert state") {
		t.Fatalf("pending native Undo was not refused: %v", err)
	}
	if adapter.server != nil {
		t.Fatal("refused worker retained an owned server")
	}
	// Inspect persisted history through a separate, explicitly owned observer
	// server after the worker has correctly closed its failed-turn process.
	observer, err := startOpenCodeOwnedServer(ctx, adapter.workDir)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	var after any
	request(observer.baseURL, http.MethodGet, messagePath, nil, &after)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("managed preparation changed existing undoable native history")
	}
	persisted, err := opencodeclient.New(observer.baseURL, adapter.workDir).GetSession(ctx, id)
	if err != nil || !bytes.Equal(persisted.Revert, reverted.Revert) {
		t.Fatalf("managed preparation cleared or changed native undo state: %v", err)
	}
	t.Log("actual native Undo retained exact existing history and revert state; managed prompt was refused")
}
