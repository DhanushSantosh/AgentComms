package app

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

func TestVersionEnvelope(t *testing.T) {
	var out, err bytes.Buffer
	if e := Run([]string{"version", "--json"}, &out, &err); e != nil {
		t.Fatal(e)
	}
	var v Envelope
	if e := json.Unmarshal(out.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	if !v.OK || v.APIVersion != APIVersion {
		t.Fatalf("bad envelope: %#v", v)
	}
}
func TestInitRequiresGitAndNonInteractiveOwner(t *testing.T) {
	d := t.TempDir()
	var out, err bytes.Buffer
	e := Run([]string{"init", "--project", d, "--non-interactive", "--owner", "owner", "--json"}, &out, &err)
	if e == nil {
		t.Fatal("non-Git init succeeded")
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = d
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(string(b))
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(d, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(d, "credentials"))
	out.Reset()
	err.Reset()
	if e = Run([]string{"init", "--project", d, "--non-interactive", "--owner", "owner", "--json"}, &out, &err); e != nil {
		t.Fatal(e)
	}
}
func TestCompletion(t *testing.T) {
	var out, err bytes.Buffer
	if e := Run([]string{"completion", "powershell"}, &out, &err); e != nil {
		t.Fatal(e)
	}
	if out.Len() < 100 {
		t.Fatal("completion output too small")
	}
}

func TestDoctorReportsRuntimeAndBootstrapProblems(t *testing.T) {
	d := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = d
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(string(b))
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(d, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(d, "credentials"))
	var out, err bytes.Buffer
	if e := Run([]string{"init", "--project", d, "--non-interactive", "--owner", "owner", "--json"}, &out, &err); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	err.Reset()
	if e := Run([]string{"agent", "register", "--project", d, "--id", "builder", "--json"}, &out, &err); e != nil {
		t.Fatal(e)
	}
	svc := service.New(d)
	if _, e := svc.Store.Append("owner", "task.create", "stale-work", model.TaskCreated{Title: "Stale work", Repository: "local", Branch: "main", Resources: []string{"path:src/**"}}); e != nil {
		t.Fatal(e)
	}
	if _, e := svc.Store.Append("owner", "task.claim", "stale-work", model.TaskClaimed{LeaseUntil: time.Now().UTC().Add(-time.Hour)}); e != nil {
		t.Fatal(e)
	}
	cfgPath := filepath.Join(d, ".agent-comms", "config.json")
	var cfg map[string]any
	b, _ := os.ReadFile(cfgPath)
	_ = json.Unmarshal(b, &cfg)
	cfg["toolkit_version"] = "0.1.0"
	b, _ = json.Marshal(cfg)
	_ = os.WriteFile(cfgPath, b, 0600)
	_ = os.Remove(filepath.Join(d, ".agents"))
	_ = os.Remove(filepath.Join(d, ".agent-comms", "AGENT_INSTRUCTIONS.md"))
	out.Reset()
	err.Reset()
	if e := Run([]string{"doctor", "--project", d, "--json"}, &out, &err); e != nil {
		t.Fatal(e)
	}
	text := out.String()
	for _, code := range []string{"BINARY_RUNTIME_VERSION_MISMATCH", "MANAGED_BOOTSTRAP_MISSING", "AGENT_INSTRUCTIONS_MISSING", "STALE_LEASE", "TEST_LIKE_RUNTIME"} {
		if !bytes.Contains(out.Bytes(), []byte(code)) {
			t.Fatalf("doctor missing %s: %s", code, text)
		}
	}
}

func TestIncompleteAdoptionBlocksNormalCommands(t *testing.T) {
	d := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = d
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(string(b))
	}
	if e := os.WriteFile(filepath.Join(d, ".agents"), []byte("legacy coordination"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(d, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(d, "credentials"))
	var out, err bytes.Buffer
	if e := Run([]string{"migrate", "adopt", "--project", d, "--owner", "owner", "--json"}, &out, &err); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	err.Reset()
	if e := Run([]string{"task", "list", "--project", d, "--json"}, &out, &err); e == nil {
		t.Fatal("normal task command was allowed before activation")
	}
	if !bytes.Contains(err.Bytes(), []byte("CUTOVER_INCOMPLETE")) && !bytes.Contains(err.Bytes(), []byte("cutover is incomplete")) {
		t.Fatalf("unexpected error: %s", err.String())
	}
}
