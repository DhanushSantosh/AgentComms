package opencodeclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestSubscribePreservesDirectoryBoundary(t *testing.T) {
	for _, directory := range []string{"/synthetic owned/project", ""} {
		t.Run(directory, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/event" || r.Header.Get("x-opencode-directory") != url.PathEscape(directory) {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			events, err := Subscribe(ctx, New(server.URL, directory))
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			select {
			case _, ok := <-events:
				if ok {
					t.Fatal("unexpected event")
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled subscription did not close")
			}
		})
	}
}

func TestPermissionWatcherExactSessionBoundary(t *testing.T) {
	replies := map[string]string{}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Reply string `json:"reply"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		if _, exists := replies[r.URL.Path]; exists {
			t.Errorf("duplicate reply: %s", r.URL.Path)
		}
		replies[r.URL.Path] = body.Reply
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := New(server.URL, "")
	aGovernance, bGovernance := &fixedApprover{}, &fixedApprover{}
	aGates, bGates := 0, 0
	a := NewPermissionWatcher(client, "ses-a", func() bool { aGates++; return true }, aGovernance)
	b := NewPermissionWatcher(client, "ses-b", func() bool { bGates++; return false }, bGovernance)
	requests := []string{
		`{"id":"edit-a","sessionID":"ses-a","permission":"edit"}`,
		`{"id":"edit-b","sessionID":"ses-b","permission":"edit"}`,
		`{"id":"bash-a","sessionID":"ses-a","permission":"bash"}`,
		`{"id":"read-b","sessionID":"ses-b","permission":"read"}`,
		`{"id":"foreign","sessionID":"ses-c","permission":"bash"}`,
		`{"id":"absent","permission":"edit"}`,
		`{"id":"empty","sessionID":"","permission":"edit"}`,
		`{"id":"null","sessionID":null,"permission":"edit"}`,
		`{"id":"number","sessionID":1,"permission":"edit"}`,
		`{"id":"object","sessionID":{},"permission":"edit"}`,
		`{"id":"boolean","sessionID":true,"permission":"edit"}`,
		`{"id":"array","sessionID":[],"permission":"edit"}`,
		`{"id":"case","sessionID":"SES-A","permission":"edit"}`,
		`{"id":"space","sessionID":"ses-a ","permission":"edit"}`,
		`{"sessionID":"ses-a","permission":"edit"}`,
		`not-json`,
	}
	// Run synchronously to use channel closure as a deterministic processing barrier.
	for _, watcher := range []*PermissionWatcher{a, b, NewPermissionWatcher(client, "", func() bool { t.Fatal("unbound edit gate reached"); return true }, nil)} {
		events := make(chan Event, len(requests))
		for _, raw := range requests {
			events <- Event{Type: "permission.asked", Properties: json.RawMessage(raw)}
		}
		close(events)
		watcher.Run(context.Background(), events)
	}
	want := map[string]string{
		"/permission/edit-a/reply": "once", "/permission/edit-b/reply": "reject",
		"/permission/bash-a/reply": "reject", "/permission/read-b/reply": "once",
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(replies, want) {
		t.Fatalf("replies = %v, want %v", replies, want)
	}
	if !reflect.DeepEqual(aGovernance.calls, []string{"bash"}) || len(bGovernance.calls) != 0 {
		t.Fatalf("governance calls a=%v b=%v", aGovernance.calls, bGovernance.calls)
	}
	if aGates != 1 || bGates != 1 {
		t.Fatalf("foreign request reached edit gates: a=%d b=%d", aGates, bGates)
	}
	if !reflect.DeepEqual(a.DeniedKinds(), []string{"bash"}) || !reflect.DeepEqual(b.DeniedKinds(), []string{"edit"}) {
		t.Fatalf("denials a=%v b=%v", a.DeniedKinds(), b.DeniedKinds())
	}
}
