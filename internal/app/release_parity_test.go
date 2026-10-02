package app

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/mcp"
	"github.com/DhanushSantosh/AgentComms/internal/testsupport"
)

// Compare the actual adapters against one isolated authority. Generated event
// times/IDs differ; the governed payloads, actors and read pages must agree.
func TestReleaseCLIMCPAuthoritativeParity(t *testing.T) {
	svc, root := testsupport.StartPersonalProject(t)
	// The CLI launches its own in-process daemon whenever its health check
	// misses the helper's (seen on Windows); stop it before TempDir cleanup,
	// or its open personal-authority.db cannot be deleted there. Registered
	// after the helper, so it runs first.
	cleanupProjectDaemon(t, root)
	cli := func(args ...string) json.RawMessage {
		t.Helper()
		var out, stderr bytes.Buffer
		args = append(args, "--project", root, "--actor", "owner", "--json")
		if err := Run(args, &out, &stderr); err != nil {
			t.Fatalf("CLI %v: %v %s", args, err, stderr.String())
		}
		return extractResult(t, out.Bytes())
	}
	rpc := func(name string, args map[string]any) json.RawMessage {
		t.Helper()
		input, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err = mcp.Serve(svc, identity.ActorResolution{Actor: "owner"}, "test", bytes.NewReader(append(input, '\n')), &out); err != nil {
			t.Fatal(err)
		}
		var reply struct {
			Result struct {
				Structured json.RawMessage `json:"structuredContent"`
			} `json:"result"`
			Error json.RawMessage `json:"error"`
		}
		if err = json.Unmarshal(out.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if len(reply.Error) != 0 {
			t.Fatalf("MCP %s: %s", name, reply.Error)
		}
		return reply.Result.Structured
	}
	cli("task", "create", "--id", "cli-task", "--title", "Parity", "--repository", "local", "--branch", "dev", "--resource", "src")
	rpc("task_create", map[string]any{"id": "mcp-task", "title": "Parity", "repository": "local", "branch": "dev", "resources": []string{"src"}})
	cli("message", "post", "--id", "cli-message", "--kind", "FYI", "--to", "owner", "--subject", "Parity", "--body", "Same body")
	rpc("message_post", map[string]any{"id": "mcp-message", "kind": "FYI", "to": []string{"owner"}, "subject": "Parity", "body": "Same body"})
	state, err := svc.State()
	if err != nil {
		t.Fatal(err)
	}
	// Check existence first: two missing entries would compare equal as zero
	// values and pass every field comparison below.
	a, okA := state.Tasks["cli-task"]
	b, okB := state.Tasks["mcp-task"]
	if !okA || !okB {
		t.Fatalf("both adapters must create their task: cli=%t mcp=%t", okA, okB)
	}
	if a.Title != b.Title || a.Status != b.Status || a.Repository != b.Repository || a.Branch != b.Branch || !reflect.DeepEqual(a.Resources, b.Resources) {
		t.Fatalf("task adapters disagree: %+v / %+v", a, b)
	}
	x, okX := state.Messages["cli-message"]
	y, okY := state.Messages["mcp-message"]
	if !okX || !okY {
		t.Fatalf("both adapters must post their message: cli=%t mcp=%t", okX, okY)
	}
	if x.Kind != y.Kind || x.From != y.From || x.Subject != y.Subject || x.Body != y.Body || !reflect.DeepEqual(x.To, y.To) {
		t.Fatalf("message adapters disagree: %+v / %+v", x, y)
	}
	// The whole log fits one 500-record page; the cursor walk below must
	// visit exactly these sequences.
	var full struct {
		Items []struct {
			Event struct {
				Sequence uint64 `json:"sequence"`
			} `json:"event"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	if err = json.Unmarshal(cli("history", "--limit", "500"), &full); err != nil {
		t.Fatal(err)
	}
	if full.NextCursor != "" || len(full.Items) < 6 {
		t.Fatalf("expected the whole history in one page: %d items, next=%q", len(full.Items), full.NextCursor)
	}
	want := map[uint64]bool{}
	for _, item := range full.Items {
		want[item.Event.Sequence] = true
	}
	for _, limit := range []int{1, 3, 500} {
		var left, right any
		cliPage := cli("history", "--limit", strconv.Itoa(limit))
		rpcPage := rpc("history", map[string]any{"limit": limit})
		if err = json.Unmarshal(cliPage, &left); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(rpcPage, &right); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(left, right) {
			t.Fatalf("history adapters disagree at limit %d", limit)
		}
	}
	// Both frontends must advance through the same cursor chain without
	// dropping or repeating an event when a page has only one record.
	seen := map[uint64]bool{}
	cursor := ""
	for {
		cliArgs := []string{"history", "--limit", "1"}
		mcpArgs := map[string]any{"limit": 1}
		if cursor != "" {
			cliArgs = append(cliArgs, "--cursor", cursor)
			mcpArgs["cursor"] = cursor
		}
		var left, right struct {
			Items []struct {
				Event struct {
					Sequence uint64 `json:"sequence"`
				} `json:"event"`
			} `json:"items"`
			NextCursor string `json:"next_cursor"`
		}
		if err = json.Unmarshal(cli(cliArgs...), &left); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(rpc("history", mcpArgs), &right); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(left, right) || len(left.Items) != 1 {
			t.Fatalf("cursor page differs: CLI=%+v MCP=%+v", left, right)
		}
		sequence := left.Items[0].Event.Sequence
		if seen[sequence] {
			t.Fatalf("history repeated sequence %v", sequence)
		}
		seen[sequence] = true
		cursor = left.NextCursor
		if cursor == "" {
			break
		}
		if len(seen) > len(want) {
			t.Fatal("history cursor did not terminate")
		}
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("cursor walk visited %d events, want exactly the %d in the full page", len(seen), len(want))
	}
}
