package service

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

func setup(t *testing.T) *Service {
	t.Helper()
	d := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = d
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git init: %s", b)
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(d, "user"))
	s := New(d)
	s.Store.SetCredentialStore(identity.NewMemoryStore())
	if e := s.Store.Init("owner"); e != nil {
		t.Fatal(e)
	}
	return s
}
func must(t *testing.T, s *Service, a, k, id string, p any) {
	t.Helper()
	if _, e := s.Execute(a, k, id, p); e != nil {
		t.Fatalf("%s: %v", k, e)
	}
}
func activate(t *testing.T, s *Service, id string, pt model.PrincipalType) {
	t.Helper()
	if _, e := s.Register(id, id, pt); e != nil {
		t.Fatal(e)
	}
	must(t, s, "owner", "agent.activate", id, model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}})
}
func TestIdentityTaskOfferLeaseAndHandoff(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	activate(t, s, "beta", model.PrincipalAgent)
	must(t, s, "owner", "task.create", "task-1", model.TaskCreated{Title: "Build", Repository: "local", Branch: "feature", Resources: []string{"src/api"}})
	must(t, s, "owner", "task.offer", "task-1", model.TaskOffered{To: "alpha", ExpiresAt: time.Now().Add(time.Hour)})
	must(t, s, "alpha", "task.claim", "task-1", model.TaskClaimed{})
	must(t, s, "alpha", "task.start", "task-1", model.TaskStatus{})
	must(t, s, "alpha", "task.handoff", "task-1", model.TaskHandoff{To: "beta", Summary: "ready"})
	st, _ := s.State()
	if st.Tasks["task-1"].Owner != "alpha" {
		t.Fatal("handoff changed ownership before acceptance")
	}
	must(t, s, "beta", "task.handoff.accept", "task-1", model.TaskStatus{})
	st, _ = s.State()
	if st.Tasks["task-1"].Owner != "beta" {
		t.Fatal("handoff not accepted")
	}
}
func TestOverlappingProtectedLease(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	activate(t, s, "beta", model.PrincipalAgent)
	must(t, s, "owner", "task.create", "one", model.TaskCreated{Title: "One", Repository: "local", Branch: "a", Resources: []string{"src"}})
	must(t, s, "owner", "task.create", "two", model.TaskCreated{Title: "Two", Repository: "local", Branch: "b", Resources: []string{"src/file.go"}})
	must(t, s, "alpha", "task.claim", "one", model.TaskClaimed{})
	if _, e := s.Execute("beta", "task.claim", "two", model.TaskClaimed{}); e == nil {
		t.Fatal("overlap was allowed")
	}
}
func TestTypedMessageObligations(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	must(t, s, "owner", "message.post", "m1", model.MessagePosted{Kind: "ACTION", To: []string{"alpha"}, Subject: "Run checks"})
	must(t, s, "alpha", "message.ack", "m1", model.MessageResponse{})
	st, _ := s.State()
	if st.Messages["m1"].Recipients[0].Status != "ACCEPTED" {
		t.Fatal("action was not accepted")
	}
	must(t, s, "alpha", "message.complete", "m1", model.MessageResponse{})
	st, _ = s.State()
	if st.Messages["m1"].Status != "SATISFIED" {
		t.Fatal("action not satisfied")
	}
}
func TestHumanApprovalPolicy(t *testing.T) {
	s := setup(t)
	activate(t, s, "bot", model.PrincipalAgent)
	must(t, s, "owner", "approval.request", "a1", model.ApprovalRequested{Tier: "HUMAN", Action: "delete external data", Reason: "cleanup"})
	if _, e := s.Execute("bot", "approval.approve", "a1", model.ApprovalResponse{}); e == nil {
		t.Fatal("agent approved human tier")
	}
	must(t, s, "owner", "approval.approve", "a1", model.ApprovalResponse{})
}
func TestConcurrentWritersAndIntegrity(t *testing.T) {
	s := setup(t)
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := s.Execute("owner", "decision.create", string(rune('a'+i)), model.DecisionPayload{Title: "Decision", Statement: "Synthetic"})
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if e := s.Store.Verify(); e != nil {
		t.Fatal(e)
	}
}

func TestConcurrentClaimsRevalidateInsideTransaction(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	activate(t, s, "beta", model.PrincipalAgent)
	must(t, s, "owner", "task.create", "exclusive", model.TaskCreated{
		Title: "Exclusive work", Repository: "local", Branch: "feature",
		Resources: []string{"src/exclusive"},
	})

	start := make(chan struct{})
	results := make(chan error, 2)
	var writers sync.WaitGroup
	for _, actor := range []string{"alpha", "beta"} {
		writers.Add(1)
		go func(actor string) {
			defer writers.Done()
			<-start
			_, err := s.Execute(actor, "task.claim", "exclusive", model.TaskClaimed{})
			results <- err
		}(actor)
	}
	close(start)
	writers.Wait()
	close(results)

	successes := 0
	failures := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent claims: successes=%d failures=%d", successes, failures)
	}
	if err := s.Store.Verify(); err != nil {
		t.Fatal(err)
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Tasks["exclusive"].Owner == "" {
		t.Fatal("successful claim did not establish an owner")
	}
}
func TestBusyError(t *testing.T) {
	s := setup(t)
	s.Store.LockTimeout = 50 * time.Millisecond
	lock := filepath.Join(s.Store.Root, store.Runtime, "tmp", "transaction.lock")
	if e := os.Mkdir(lock, 0700); e != nil {
		t.Fatal(e)
	}
	_ = os.WriteFile(filepath.Join(lock, "holder.json"), []byte(`{"pid":1,"actor":"other","since":"2099-01-01T00:00:00Z"}`), 0600)
	_, e := s.Execute("owner", "decision.create", "d", model.DecisionPayload{Title: "x", Statement: "y"})
	var busy *store.BusyError
	if !errors.As(e, &busy) {
		t.Fatalf("expected BUSY, got %v", e)
	}
}
func TestTamperDetection(t *testing.T) {
	s := setup(t)
	p := filepath.Join(s.Store.Root, store.Runtime, "events", "evt-00000000000000000002.json")
	b, _ := os.ReadFile(p)
	b = []byte(strings.Replace(string(b), "agent.activate", "agent.suspend", 1))
	_ = os.WriteFile(p, b, 0600)
	if e := s.Store.Verify(); e == nil {
		t.Fatal("tamper not detected")
	}
}
func TestArtifactExportsAndRecovery(t *testing.T) {
	s := setup(t)
	p := filepath.Join(t.TempDir(), "evidence.txt")
	_ = os.WriteFile(p, []byte("evidence"), 0600)
	ev, e := s.AddArtifact("owner", p)
	if e != nil {
		t.Fatal(e)
	}
	if len(ev.EntityID) != 64 {
		t.Fatal("artifact is not addressed")
	}
	var jsonl, md bytes.Buffer
	if e = s.ExportJSONL(&jsonl); e != nil {
		t.Fatal(e)
	}
	if e = s.ExportMarkdown(&md); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(md.String(), "Integrity: **true**") {
		t.Fatal("report missing integrity")
	}
	tmp := filepath.Join(s.Store.Root, store.Runtime, "tmp", "partial.tmp")
	_ = os.WriteFile(tmp, []byte("partial"), 0600)
	if e = s.Store.Recover(); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(tmp); !os.IsNotExist(e) {
		t.Fatal("partial write survived")
	}
}

func TestActorKeyRotationPreservesVerification(t *testing.T) {
	s := setup(t)
	activate(t, s, "alpha", model.PrincipalAgent)
	before, _ := s.State()
	old := before.Agents["alpha"].KeyFingerprint
	if _, err := s.RotateKey("alpha"); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Verify(); err != nil {
		t.Fatal(err)
	}
	after, _ := s.State()
	if after.Agents["alpha"].KeyFingerprint == old {
		t.Fatal("fingerprint did not rotate")
	}
}
