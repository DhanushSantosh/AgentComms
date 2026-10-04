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

type nativePolicyFixture struct {
	mu                                 sync.Mutex
	root, id, fault                    string
	rules                              []opencodeclient.PermissionRule
	markers                            map[string]opencodeclient.PreparationMessage
	starts, closes, prompts, disposals int
	server                             *httptest.Server
	disposed                           chan struct{}
}

func newNativePolicyFixture(t *testing.T) (*nativePolicyFixture, Config) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &nativePolicyFixture{root: canonical, id: "ses_" + openCodeIdentityHash(root), markers: map[string]opencodeclient.PreparationMessage{}, rules: []opencodeclient.PermissionRule{{Permission: "edit", Pattern: "private/*", Action: "deny"}}, disposed: make(chan struct{}, 1)}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(fixture.server.Close)
	return fixture, Config{Actor: "synthetic", RuntimeID: "runtime-owned", WorkDir: root, PermissionMode: "plan", Status: func(string) {}}
}

func (f *nativePolicyFixture) adapter(t *testing.T) *openCodeLiveAdapter {
	t.Helper()
	adapter := &openCodeLiveAdapter{start: func(context.Context, string) (*ownedLiveServer, error) {
		f.mu.Lock()
		f.starts++
		f.mu.Unlock()
		ctx, cancel := context.WithCancel(context.Background())
		server := &ownedLiveServer{baseURL: f.server.URL, cancel: cancel, done: make(chan struct{})}
		go func() { <-ctx.Done(); f.mu.Lock(); f.closes++; f.mu.Unlock(); close(server.done) }()
		return server, nil
	}}
	t.Cleanup(func() {
		if err := adapter.Close(); err != nil {
			t.Error(err)
		}
	})
	return adapter
}

func (f *nativePolicyFixture) serve(w http.ResponseWriter, r *http.Request) {
	directory, err := url.PathUnescape(r.Header.Get("x-opencode-directory"))
	if err != nil || directory != f.root {
		http.Error(w, "wrong directory", 400)
		return
	}
	if r.URL.Path == "/event" {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		return
	}
	if r.URL.Path == "/global/event" {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data:{\"payload\":{\"type\":\"server.heartbeat\",\"properties\":{}}}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-f.disposed:
			payload, _ := json.Marshal(map[string]any{"directory": f.root, "payload": map[string]any{"type": "server.instance.disposed", "properties": map[string]string{"directory": f.root}}})
			fmt.Fprintf(w, "data: %s\n\n", payload)
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
			return
		}
		<-r.Context().Done()
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	respondSession := func() {
		session := opencodeclient.Session{ID: f.id, Directory: f.root, Permission: f.rules}
		if f.fault == "foreign-session" {
			session.ID = "ses_foreign"
		}
		if f.fault == "foreign-directory" {
			session.Directory = filepath.Join(f.root, "foreign")
		}
		if f.fault == "native-revert" {
			session.Revert = json.RawMessage(`{"messageID":"existing-user-message"}`)
		}
		json.NewEncoder(w).Encode(session)
	}
	switch {
	case r.URL.Path == "/instance/dispose":
		f.disposals++
		if f.fault == "reset" {
			http.Error(w, "synthetic reset failure", 500)
			return
		}
		fmt.Fprint(w, `true`)
		if f.fault != "reset-ack-only" {
			f.disposed <- struct{}{}
		}
	case r.URL.Path == "/agent":
		if f.fault == "agent" {
			fmt.Fprint(w, `[]`)
			return
		}
		rules := []opencodeclient.PermissionRule{{Permission: "*", Pattern: "*", Action: "allow"}}
		if f.fault == "unknown-rule" {
			rules[0].Action = "unsupported"
		}
		json.NewEncoder(w).Encode([]opencodeclient.Agent{{Name: "build", Mode: "primary", Permission: rules}})
	case r.URL.Path == "/session" || r.URL.Path == "/session/"+f.id:
		if f.fault == "lookup" {
			http.Error(w, "synthetic lookup failure", 500)
			return
		}
		if r.Method == http.MethodPatch {
			if f.fault == "patch" {
				http.Error(w, "synthetic patch failure", 500)
				return
			}
			var body struct {
				Permission []opencodeclient.PermissionRule `json:"permission"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			f.rules = append(f.rules, body.Permission...)
			if f.fault == "patch-loss" {
				http.Error(w, "synthetic lost patch reply", 500)
				return
			}
			if f.fault == "readback" {
				f.rules = append(f.rules, opencodeclient.PermissionRule{Permission: "*", Pattern: "*", Action: "allow"})
			}
		}
		respondSession()
	case r.URL.Path == "/session/"+f.id+"/message":
		var body struct {
			NoReply   bool                      `json:"noReply"`
			MessageID string                    `json:"messageID"`
			Agent     string                    `json:"agent"`
			Tools     map[string]bool           `json:"tools"`
			Parts     []opencodeclient.TextPart `json:"parts"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.NoReply {
			if body.Tools["*"] || len(body.Tools) != 1 || body.Parts == nil || len(body.Parts) != 0 {
				http.Error(w, "unsafe preparation", 400)
				return
			}
			f.rules = []opencodeclient.PermissionRule{{Permission: "*", Pattern: "*", Action: "deny"}}
			marker := opencodeclient.PreparationMessage{ID: body.MessageID, SessionID: f.id, Agent: "build", Role: "user"}
			f.markers[body.MessageID] = marker
			if f.fault == "prepare-loss" {
				http.Error(w, "synthetic lost preparation reply", 500)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"info": marker, "parts": []opencodeclient.Part{}})
			return
		}
		if body.Agent != "build" || len(f.markers) != 0 {
			http.Error(w, "unprepared prompt", 400)
			return
		}
		if f.fault == "prompt-block" {
			<-r.Context().Done()
			return
		}
		f.prompts++
		json.NewEncoder(w).Encode(opencodeclient.PromptResponse{Parts: []opencodeclient.Part{{Type: "text", Text: "verified synthetic receipt"}}})
	case strings.HasPrefix(r.URL.Path, "/session/"+f.id+"/message/"):
		id := strings.TrimPrefix(r.URL.Path, "/session/"+f.id+"/message/")
		marker, found := f.markers[id]
		if !found {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodDelete {
			if f.fault == "cleanup" {
				http.Error(w, "synthetic cleanup failure", 500)
				return
			}
			if f.fault == "cleanup-unconfirmed" {
				fmt.Fprint(w, `false`)
				return
			}
			delete(f.markers, id)
			if f.fault == "cleanup-loss" {
				http.Error(w, "synthetic lost cleanup reply", 500)
				return
			}
			fmt.Fprint(w, `true`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"info": marker, "parts": []opencodeclient.Part{}})
	default:
		http.NotFound(w, r)
	}
}

func TestOpenCodePolicyTwoTurnsAndRestartPreserveOriginal(t *testing.T) {
	f, config := newNativePolicyFixture(t)
	adapter := f.adapter(t)
	for i := 0; i < 2; i++ {
		if _, err := adapter.Execute(context.Background(), config, model.Invocation{ID: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := f.adapter(t)
	if _, err := restarted.Execute(context.Background(), config, model.Invocation{ID: "restart"}); err != nil {
		t.Fatal(err)
	}
	var record openCodePolicyRecord
	found, err := readOpenCodeRecord(restarted.recordPath, &record)
	if err != nil || !found || record.PendingMarker != "" || len(record.Original) != 1 || record.Original[0].Action != "deny" {
		t.Fatalf("original/recovery record changed: %+v err=%v", record, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.starts != 2 || f.closes != 1 || f.disposals != 3 || f.prompts != 3 || len(f.rules) != 4 || len(f.markers) != 0 {
		t.Fatalf("turn preparation accumulated rules or leaked lifecycle: starts=%d closes=%d resets=%d prompts=%d rules=%d markers=%d", f.starts, f.closes, f.disposals, f.prompts, len(f.rules), len(f.markers))
	}
}

func TestOpenCodePolicyFailuresPreventPromptAndCloseOwnedServer(t *testing.T) {
	for _, fault := range []string{"reset", "lookup", "foreign-session", "foreign-directory", "native-revert", "agent", "unknown-rule", "prepare-loss", "patch", "patch-loss", "readback", "cleanup", "cleanup-unconfirmed", "cleanup-loss"} {
		t.Run(fault, func(t *testing.T) {
			f, config := newNativePolicyFixture(t)
			f.fault = fault
			adapter := f.adapter(t)
			if _, err := adapter.Execute(context.Background(), config, model.Invocation{ID: "failure"}); err == nil {
				t.Fatal("unsafe preparation reached prompt")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.prompts != 0 || f.closes != 1 || adapter.server != nil {
				t.Fatal("failed preparation prompted or leaked owned server")
			}
			if fault == "native-revert" && len(f.markers) != 0 {
				t.Fatal("native undo state reached synthetic preparation")
			}
		})
	}
}

func TestOpenCodePolicyUnconfirmedResetPreventsPrompt(t *testing.T) {
	f, config := newNativePolicyFixture(t)
	f.fault = "reset-ack-only"
	adapter := f.adapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := adapter.Execute(ctx, config, model.Invocation{ID: "reset-not-complete"}); err == nil {
		t.Fatal("acknowledgment-only reset permitted execution")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prompts != 0 || len(f.markers) != 0 || f.closes != 1 || adapter.server != nil {
		t.Fatal("unconfirmed reset reached preparation/prompt or leaked owned resources")
	}
}

func TestOpenCodePolicyLostResponsesRecoverExactMarker(t *testing.T) {
	for _, fault := range []string{"prepare-loss", "patch-loss", "cleanup-loss"} {
		t.Run(fault, func(t *testing.T) {
			f, config := newNativePolicyFixture(t)
			f.fault = fault
			adapter := f.adapter(t)
			if _, err := adapter.Execute(context.Background(), config, model.Invocation{ID: "interrupted"}); err == nil {
				t.Fatal("lost response accepted")
			}
			// Recovery must work from durable metadata, not in-memory assumptions.
			if err := adapter.Close(); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			f.fault = ""
			f.mu.Unlock()
			restarted := f.adapter(t)
			if _, err := restarted.Execute(context.Background(), config, model.Invocation{ID: "recovery"}); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.prompts != 1 || len(f.markers) != 0 || len(f.rules) != 4 {
				t.Fatal("recovery lost native restrictions or retained a marker")
			}
		})
	}
}

func TestOpenCodePolicyRejectsConcurrentOrForeignOwnership(t *testing.T) {
	f, config := newNativePolicyFixture(t)
	first := f.adapter(t)
	if _, err := first.Execute(context.Background(), config, model.Invocation{ID: "first"}); err != nil {
		t.Fatal(err)
	}
	second := f.adapter(t)
	if _, err := second.Execute(context.Background(), config, model.Invocation{ID: "concurrent"}); err == nil {
		t.Fatal("concurrent same runtime accepted")
	}
	config.RuntimeID = "foreign-runtime"
	config.SessionID = f.id
	if _, err := second.Execute(context.Background(), config, model.Invocation{ID: "foreign"}); err == nil {
		t.Fatal("foreign owned session accepted")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Execute(context.Background(), config, model.Invocation{ID: "foreign-after-close"}); err == nil {
		t.Fatal("durable session ownership discarded after worker exit")
	}
}

func TestOpenCodePolicySameRuntimeIDIsIsolatedAcrossProjects(t *testing.T) {
	firstFixture, firstConfig := newNativePolicyFixture(t)
	secondFixture, secondConfig := newNativePolicyFixture(t)
	// Deliberately share one OS-user configuration namespace and logical ID.
	// Only project identity may distinguish these workers' durable records.
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "shared-user"))
	if firstConfig.RuntimeID != secondConfig.RuntimeID {
		t.Fatal("fixture did not retain the same logical runtime ID")
	}
	first := firstFixture.adapter(t)
	second := secondFixture.adapter(t)
	for _, turn := range []struct {
		adapter *openCodeLiveAdapter
		config  Config
		id      string
	}{{first, firstConfig, "first-project"}, {second, secondConfig, "second-project"}, {first, firstConfig, "first-project-resume"}} {
		if _, err := turn.adapter.Execute(context.Background(), turn.config, model.Invocation{ID: turn.id}); err != nil {
			t.Fatalf("project-scoped runtime failed: %v", err)
		}
	}
	if first.recordPath == second.recordPath || first.sessionID == second.sessionID || first.server.baseURL == second.server.baseURL {
		t.Fatal("two projects shared policy, native session, or owned server")
	}
	firstFixture.mu.Lock()
	firstPrompts := firstFixture.prompts
	firstFixture.mu.Unlock()
	secondFixture.mu.Lock()
	secondPrompts := secondFixture.prompts
	secondFixture.mu.Unlock()
	if firstPrompts != 2 || secondPrompts != 1 {
		t.Fatal("project work crossed runtime ownership boundaries")
	}
}

func TestOpenCodePolicyExternalChangesFailClosed(t *testing.T) {
	f, config := newNativePolicyFixture(t)
	adapter := f.adapter(t)
	if _, err := adapter.Execute(context.Background(), config, model.Invocation{ID: "first"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.rules = append(f.rules, opencodeclient.PermissionRule{Permission: "read", Pattern: "secret/*", Action: "deny"})
	before := append([]opencodeclient.PermissionRule(nil), f.rules...)
	f.mu.Unlock()
	if _, err := adapter.Execute(context.Background(), config, model.Invocation{ID: "changed"}); err == nil {
		t.Fatal("unknown operator policy silently reinterpreted")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !sameOpenCodeRules(before, f.rules) || f.prompts != 1 {
		t.Fatal("operator rules discarded or new prompt executed")
	}
}

func TestOpenCodePolicyRecoveryRejectsModifiedMarkerAndRules(t *testing.T) {
	for _, changed := range []string{"marker", "rules"} {
		t.Run(changed, func(t *testing.T) {
			f, config := newNativePolicyFixture(t)
			f.fault = "prepare-loss"
			adapter := f.adapter(t)
			if _, err := adapter.Execute(context.Background(), config, model.Invocation{ID: "lost"}); err == nil {
				t.Fatal("lost response accepted")
			}
			if err := adapter.Close(); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			f.fault = ""
			if changed == "rules" {
				f.rules = append(f.rules, opencodeclient.PermissionRule{Permission: "read", Pattern: "secret/*", Action: "deny"})
			}
			if changed == "marker" {
				for id, marker := range f.markers {
					marker.SessionID = "ses_foreign"
					f.markers[id] = marker
				}
			}
			f.mu.Unlock()
			restarted := f.adapter(t)
			if _, err := restarted.Execute(context.Background(), config, model.Invocation{ID: "recovery"}); err == nil {
				t.Fatal("modified recovery state accepted")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.prompts != 0 || len(f.markers) != 1 {
				t.Fatal("unrecognized state was prompted or a message was deleted")
			}
		})
	}
}

func TestOpenCodePolicyCancellationStopsOwnedServerAndNextTurnRecovers(t *testing.T) {
	f, config := newNativePolicyFixture(t)
	f.fault = "prompt-block"
	adapter := f.adapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := adapter.Execute(ctx, config, model.Invocation{ID: "cancelled"}); err == nil {
		t.Fatal("cancelled prompt returned success")
	}
	f.mu.Lock()
	if f.closes != 1 || adapter.server != nil {
		t.Error("cancelled invocation retained its owned server")
	}
	f.fault = ""
	f.mu.Unlock()
	if _, err := adapter.Execute(context.Background(), config, model.Invocation{ID: "next"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.starts != 2 || f.prompts != 1 {
		t.Fatal("later turn failed to recover with a new owned server")
	}
}

func TestOpenCodeWorkerRunExitClosesAdapter(t *testing.T) {
	instance, root := workerService(t)
	worker, err := New(Config{Service: instance, Actor: "claude-axiom", RuntimeID: "runtime-axiom", Adapter: "opencode-live", WorkDir: root, ListenWait: time.Second, Once: true})
	if err != nil {
		t.Fatal(err)
	}
	worker.run = func(context.Context, model.Invocation) (string, error) { return "synthetic", nil }
	if err := worker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !worker.adapter.(*openCodeLiveAdapter).closed {
		t.Fatal("one-shot Run exit did not close lifecycle")
	}
	other, err := resolveAdapter("opencode-live")
	if err != nil || other == worker.adapter {
		t.Fatal("workers share mutable adapter state")
	}
}
