package store

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
)

func initProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git init: %s", out)
	}
	return root
}

func TestInitNeverOverwritesExistingAgents(t *testing.T) {
	root := initProject(t)
	original := []byte("LEGACY MUST SURVIVE\r\n\x00exact bytes\n")
	path := filepath.Join(root, ".agents")
	if e := os.WriteFile(path, original, 0600); e != nil {
		t.Fatal(e)
	}
	before := sha256.Sum256(original)
	s := Open(root)
	s.SetCredentialStore(identity.NewMemoryStore())
	if e := s.Init("owner"); e == nil {
		t.Fatal("init overwrote or adopted legacy .agents without explicit flow")
	}
	after, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if sha256.Sum256(after) != before || !bytes.Equal(after, original) {
		t.Fatal("legacy .agents changed")
	}
	if _, e = os.Stat(filepath.Join(root, Runtime)); !os.IsNotExist(e) {
		t.Fatal("runtime was created despite legacy .agents refusal")
	}
}

func TestInitRollsBackInterruptedPublication(t *testing.T) {
	for _, point := range []string{"before-runtime-publish", "before-bootstrap-publish"} {
		t.Run(point, func(t *testing.T) {
			root := initProject(t)
			t.Setenv("AGENT_COMMS_TEST_INIT_FAIL_AT", point)
			t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user-config"))
			s := Open(root)
			creds := identity.NewMemoryStore()
			s.SetCredentialStore(creds)
			if e := s.Init("owner"); e == nil {
				t.Fatal("injected failure did not fail")
			}
			if _, e := os.Stat(filepath.Join(root, Runtime)); !os.IsNotExist(e) {
				t.Fatal("partial runtime survived")
			}
			if _, e := os.Stat(filepath.Join(root, ".agents")); !os.IsNotExist(e) {
				t.Fatal("partial bootstrap survived")
			}
			stages, _ := filepath.Glob(filepath.Join(root, ".agent-comms.init-*"))
			if len(stages) != 0 {
				t.Fatalf("staging directories survived: %v", stages)
			}
		})
	}
}

func TestProcessLockHelper(t *testing.T) {
	root := os.Getenv("AGENT_COMMS_LOCK_HELPER_ROOT")
	if root == "" {
		return
	}
	s := Open(root)
	release, e := s.acquire("other-process")
	if e != nil {
		os.Exit(11)
	}
	if e = os.WriteFile(os.Getenv("AGENT_COMMS_LOCK_READY"), []byte("ready"), 0600); e != nil {
		os.Exit(12)
	}
	for {
		if _, e = os.Stat(os.Getenv("AGENT_COMMS_LOCK_STOP")); e == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	release()
}

func TestCutoverHonorsCrossProcessLock(t *testing.T) {
	root := initProject(t)
	if e := os.WriteFile(filepath.Join(root, ".agents"), []byte("legacy"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	s := Open(root)
	s.SetCredentialStore(identity.NewMemoryStore())
	if _, e := s.PrepareLegacyAdoption("owner"); e != nil {
		t.Fatal(e)
	}
	ready := filepath.Join(root, "helper.ready")
	stop := filepath.Join(root, "helper.stop")
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessLockHelper$")
	cmd.Env = append(os.Environ(), "AGENT_COMMS_LOCK_HELPER_ROOT="+root, "AGENT_COMMS_LOCK_READY="+ready, "AGENT_COMMS_LOCK_STOP="+stop)
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		_ = os.WriteFile(stop, []byte("stop"), 0600)
		_ = cmd.Wait()
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, e := os.Stat(ready); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lock helper did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.LockTimeout = 100 * time.Millisecond
	_, e := s.ConfirmLegacySeeding("owner")
	var busy *BusyError
	if !errors.As(e, &busy) {
		t.Fatalf("expected cross-process BUSY, got %v", e)
	}
}
