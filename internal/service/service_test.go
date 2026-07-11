package service

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func setup(t *testing.T) *Service {
	t.Helper()
	d := t.TempDir()
	s := New(d)
	if e := s.Store.Init("owner"); e != nil {
		t.Fatal(e)
	}
	return s
}
func exec(t *testing.T, s *Service, a, k, id string, d map[string]any) {
	t.Helper()
	if _, e := s.Execute(a, k, id, d); e != nil {
		t.Fatal(e)
	}
}
func TestLifecycleAndTwoPhaseHandoff(t *testing.T) {
	s := setup(t)
	exec(t, s, "a", "agent.register", "a", nil)
	exec(t, s, "owner", "agent.activate", "a", map[string]any{"capabilities": []string{"go"}, "scopes": []string{"src"}})
	exec(t, s, "b", "agent.register", "b", nil)
	exec(t, s, "owner", "agent.activate", "b", map[string]any{})
	exec(t, s, "owner", "task.create", "t", map[string]any{"resources": []string{"src/x"}})
	exec(t, s, "a", "task.claim", "t", map[string]any{})
	exec(t, s, "a", "task.handoff", "t", map[string]any{"to": "b"})
	st, _ := s.State()
	if st.Tasks["t"].Owner != "a" {
		t.Fatal("ownership changed before acceptance")
	}
	exec(t, s, "b", "task.handoff.accept", "t", nil)
	st, _ = s.State()
	if st.Tasks["t"].Owner != "b" {
		t.Fatal("handoff not accepted")
	}
}
func TestOverlappingLeaseBlocked(t *testing.T) {
	s := setup(t)
	for _, a := range []string{"a", "b"} {
		exec(t, s, a, "agent.register", a, nil)
		exec(t, s, "owner", "agent.activate", a, map[string]any{})
	}
	exec(t, s, "owner", "task.create", "one", map[string]any{"resources": []string{"src"}})
	exec(t, s, "owner", "task.create", "two", map[string]any{"resources": []string{"src/file.go"}})
	exec(t, s, "a", "task.claim", "one", map[string]any{})
	if _, e := s.Execute("b", "task.claim", "two", map[string]any{}); e == nil {
		t.Fatal("expected overlap rejection")
	}
}
func TestTamperDetected(t *testing.T) {
	s := setup(t)
	exec(t, s, "x", "agent.register", "x", nil)
	p := filepath.Join(s.Store.Root, ".agent-comms", "events", "evt-00000000000000000001.json")
	b, _ := os.ReadFile(p)
	b = []byte(strings.Replace(string(b), "agent.register", "agent.suspend", 1))
	_ = os.WriteFile(p, b, 0600)
	if e := s.Store.Verify(); e == nil {
		t.Fatal("expected tamper detection")
	}
}
func TestConcurrentWritersSerialize(t *testing.T) {
	s := setup(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := s.Execute("agent", "message.post", string(rune('a'+i)), map[string]any{"kind": "FYI"})
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
func TestApprovalAndRouting(t *testing.T) {
	s := setup(t)
	exec(t, s, "a", "approval.request", "p", map[string]any{"kind": "shared-write", "affected": []string{"b"}})
	exec(t, s, "owner", "approval.approve", "p", nil)
	exec(t, s, "a", "message.post", "m", map[string]any{"kind": "ACTION", "to": []string{"b"}})
	exec(t, s, "b", "message.ack", "m", nil)
	st, _ := s.State()
	if st.Approvals["p"].Status != "APPROVED" || st.Messages["m"].Status != "ACKNOWLEDGED" {
		t.Fatal("projection mismatch")
	}
}
func TestArtifactPolicyAndHash(t *testing.T) {
	s := setup(t)
	p := filepath.Join(t.TempDir(), "evidence.txt")
	_ = os.WriteFile(p, []byte("evidence"), 0600)
	e, er := s.AddArtifact("a", p)
	if er != nil {
		t.Fatal(er)
	}
	if len(e.EntityID) != 64 {
		t.Fatal("not content addressed")
	}
}
func TestCrashRecoveryRemovesTemp(t *testing.T) {
	s := setup(t)
	p := filepath.Join(s.Store.Root, ".agent-comms", "tmp", "partial.tmp")
	_ = os.WriteFile(p, []byte("partial"), 0600)
	if e := s.Store.Recover(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(p); !os.IsNotExist(e) {
		t.Fatal("temp survived recovery")
	}
}
func TestSummaryCap(t *testing.T) {
	s := setup(t)
	_, e := s.Execute("a", "message.post", "m", map[string]any{"kind": "FYI", "summary": strings.Repeat("x", 1201)})
	if e == nil {
		t.Fatal("expected summary limit")
	}
}
