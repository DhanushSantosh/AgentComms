package opencodeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer is a minimal stand-in for opencode serve's REST + SSE surface,
// covering exactly the endpoints this package uses.
type fakeServer struct {
	mu          sync.Mutex
	sessions    map[string]Session
	permissions map[string]PermissionRequest
	replies     map[string]string
	sseClients  []chan Event
	promptText  string // canned response text for Prompt calls
}

func newFakeServer() *fakeServer {
	return &fakeServer{
		sessions:    map[string]Session{},
		permissions: map[string]PermissionRequest{},
		replies:     map[string]string{},
		promptText:  "ok",
	}
}

func (f *fakeServer) broadcast(event Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ch := range f.sseClients {
		select {
		case ch <- event:
		default:
		}
	}
}

func (f *fakeServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			session := Session{ID: "ses_test1", Directory: body["directory"]}
			f.mu.Lock()
			f.sessions[session.ID] = session
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(session)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/session/") && !strings.Contains(r.URL.Path, "/message"):
			id := strings.TrimPrefix(r.URL.Path, "/session/")
			f.mu.Lock()
			session, ok := f.sessions[id]
			f.mu.Unlock()
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(session)
		case r.Method == http.MethodGet && r.URL.Path == "/session":
			f.mu.Lock()
			defer f.mu.Unlock()
			_ = json.NewEncoder(w).Encode([]Session{})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/message"):
			f.mu.Lock()
			text := f.promptText
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(PromptResponse{Parts: []Part{{Type: "text", Text: text}}})
		case r.Method == http.MethodGet && r.URL.Path == "/permission":
			f.mu.Lock()
			defer f.mu.Unlock()
			var out []PermissionRequest
			for _, p := range f.permissions {
				out = append(out, p)
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/permission/") && strings.HasSuffix(r.URL.Path, "/reply"):
			parts := strings.Split(r.URL.Path, "/")
			requestID := parts[2]
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.replies[requestID] = body["reply"]
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(true)
		case r.Method == http.MethodGet && r.URL.Path == "/event":
			f.serveSSE(w, r)
		default:
			http.Error(w, "unhandled: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}
}

func (f *fakeServer) serveSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan Event, 8)
	f.mu.Lock()
	f.sseClients = append(f.sseClients, ch)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	for {
		select {
		case event := <-ch:
			raw, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// askPermission simulates the agent raising a mid-turn permission request.
func (f *fakeServer) askPermission(id, sessionID, permission string) {
	req := PermissionRequest{ID: id, SessionID: sessionID, Permission: permission}
	f.mu.Lock()
	f.permissions[id] = req
	f.mu.Unlock()
	raw, _ := json.Marshal(req)
	f.broadcast(Event{ID: "evt_" + id, Type: "permission.asked", Properties: raw})
}

func (f *fakeServer) replyFor(requestID string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply, ok := f.replies[requestID]
	return reply, ok
}

func TestCreateAndGetSession(t *testing.T) {
	fake := newFakeServer()
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	client := New(server.URL)

	ctx := context.Background()
	created, err := client.CreateSession(ctx, "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Directory != "/tmp/project" {
		t.Fatalf("unexpected session: %+v", created)
	}
	fetched, err := client.GetSession(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.ID != created.ID {
		t.Fatalf("expected to fetch the same session, got %+v", fetched)
	}
}

func TestGetSessionMissingReturnsError(t *testing.T) {
	fake := newFakeServer()
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	client := New(server.URL)
	if _, err := client.GetSession(context.Background(), "ses_missing"); err == nil {
		t.Fatal("expected an error for a missing session")
	}
}

func TestPromptReturnsConcatenatedText(t *testing.T) {
	fake := newFakeServer()
	fake.promptText = "hello world"
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	client := New(server.URL)

	resp, err := client.Prompt(context.Background(), "ses_test1", PromptRequest{
		Parts:  []TextPart{NewTextPart("hi")},
		System: "you are a test agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text() != "hello world" {
		t.Fatalf("unexpected response text: %q", resp.Text())
	}
}

func TestHealthReportsServerReachability(t *testing.T) {
	fake := newFakeServer()
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	client := New(server.URL)
	if err := client.Health(context.Background()); err != nil {
		t.Fatal(err)
	}

	unreachable := New("http://127.0.0.1:1")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := unreachable.Health(ctx); err == nil {
		t.Fatal("expected an error for an unreachable server")
	}
}

func TestListAndReplyPermission(t *testing.T) {
	fake := newFakeServer()
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	client := New(server.URL)

	fake.askPermission("per_1", "ses_test1", "bash")
	ctx := context.Background()
	requests, err := client.ListPermissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].Permission != "bash" {
		t.Fatalf("unexpected pending permissions: %+v", requests)
	}
	if err := client.ReplyPermission(ctx, "per_1", "reject", ""); err != nil {
		t.Fatal(err)
	}
	if reply, ok := fake.replyFor("per_1"); !ok || reply != "reject" {
		t.Fatalf("expected reject reply recorded, got %q ok=%v", reply, ok)
	}
}

func TestClassifyRoutesKnownAndUnknownCategories(t *testing.T) {
	cases := map[string]Decision{
		"read":       DecisionAutoApprove,
		"glob":       DecisionAutoApprove,
		"grep":       DecisionAutoApprove,
		"list":       DecisionAutoApprove,
		"lsp":        DecisionAutoApprove,
		"edit":       DecisionModeGated,
		"bash":       DecisionGoverned,
		"webfetch":   DecisionGoverned,
		"task":       DecisionGoverned,
		"totally-new-category-from-a-future-opencode-release": DecisionGoverned,
	}
	for permission, want := range cases {
		if got := Classify(permission); got != want {
			t.Errorf("Classify(%q) = %v, want %v", permission, got, want)
		}
	}
}

type fixedApprover struct {
	approve bool
	calls   []string
}

func (f *fixedApprover) Approve(_ context.Context, _ string, permission string, _ []string) (bool, error) {
	f.calls = append(f.calls, permission)
	return f.approve, nil
}

func TestPermissionWatcherAutoApprovesReadCategory(t *testing.T) {
	fake := newFakeServer()
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	client := New(server.URL)

	approver := &fixedApprover{approve: false}
	watcher := NewPermissionWatcher(client, func() bool { return false }, approver)

	ctx, cancel := context.WithCancel(context.Background())
	events, err := Subscribe(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	go watcher.Run(ctx, events)
	waitForSSEClient(t, fake)

	fake.askPermission("per_read", "ses_test1", "read")
	waitForReply(t, fake, "per_read")
	cancel()

	if len(approver.calls) != 0 {
		t.Fatal("read permission should never reach governance")
	}
	if reply, _ := fake.replyFor("per_read"); reply != "once" {
		t.Fatalf("expected auto-approved read permission, got %q", reply)
	}
	if watcher.Denied() {
		t.Fatal("auto-approved request should not count as denied")
	}
}

func TestPermissionWatcherRoutesBashThroughGovernance(t *testing.T) {
	fake := newFakeServer()
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	client := New(server.URL)

	approver := &fixedApprover{approve: false}
	watcher := NewPermissionWatcher(client, func() bool { return true }, approver)

	ctx, cancel := context.WithCancel(context.Background())
	events, err := Subscribe(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	go watcher.Run(ctx, events)
	waitForSSEClient(t, fake)

	fake.askPermission("per_bash", "ses_test1", "bash")
	waitForReply(t, fake, "per_bash")
	cancel()

	if len(approver.calls) != 1 || approver.calls[0] != "bash" {
		t.Fatalf("expected bash routed through governance, got calls=%v", approver.calls)
	}
	if reply, _ := fake.replyFor("per_bash"); reply != "reject" {
		t.Fatalf("expected governance-denied reply, got %q", reply)
	}
	if !watcher.Denied() {
		t.Fatal("expected Denied to report true after a governance rejection")
	}
	if kinds := watcher.DeniedKinds(); len(kinds) != 1 || kinds[0] != "bash" {
		t.Fatalf("expected DeniedKinds to record [\"bash\"], got %v", kinds)
	}
}

func TestPermissionWatcherModeGatedEditRespectsAllowEdits(t *testing.T) {
	fake := newFakeServer()
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	client := New(server.URL)

	watcher := NewPermissionWatcher(client, func() bool { return true }, &fixedApprover{approve: false})

	ctx, cancel := context.WithCancel(context.Background())
	events, err := Subscribe(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	go watcher.Run(ctx, events)
	waitForSSEClient(t, fake)

	fake.askPermission("per_edit", "ses_test1", "edit")
	waitForReply(t, fake, "per_edit")
	cancel()

	if reply, _ := fake.replyFor("per_edit"); reply != "once" {
		t.Fatalf("expected edit approved when AllowEdits is true, got %q", reply)
	}
}

func TestPermissionWatcherResetTurnClearsDenials(t *testing.T) {
	fake := newFakeServer()
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	client := New(server.URL)

	watcher := NewPermissionWatcher(client, func() bool { return false }, &fixedApprover{approve: false})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := Subscribe(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	go watcher.Run(ctx, events)
	waitForSSEClient(t, fake)

	fake.askPermission("per_x", "ses_test1", "bash")
	waitForReply(t, fake, "per_x")
	if !watcher.Denied() {
		t.Fatal("expected a denial recorded")
	}
	watcher.ResetTurn()
	if watcher.Denied() {
		t.Fatal("expected ResetTurn to clear denial tracking")
	}
}

func waitForSSEClient(t *testing.T, fake *fakeServer) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fake.mu.Lock()
		n := len(fake.sseClients)
		fake.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for an SSE client to connect")
}

func waitForReply(t *testing.T, fake *fakeServer, requestID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := fake.replyFor(requestID); ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a reply to %s", requestID)
}
