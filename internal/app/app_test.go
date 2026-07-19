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
func TestInitInNonGitDir(t *testing.T) {
	d := t.TempDir()
	var out, err bytes.Buffer
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(d, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(d, "credentials"))
	e := Run([]string{"init", "--project", d, "--non-interactive", "--owner", "owner", "--json"}, &out, &err)
	if e != nil {
		t.Fatalf("non-Git init should succeed: %v", e)
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

func TestInvocationAndRuntimeCLIWorkflow(t *testing.T) {
	project := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		if err := Run(args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	run("init", "--non-interactive", "--owner", "owner")
	run("agent", "register", "--id", "builder")
	run("agent", "activate", "--id", "builder", "--role", "AGENT", "--scope", "src")
	run("runtime", "register", "--actor", "builder", "--id", "runtime-builder",
		"--agent", "builder", "--connector", "MCP", "--max-concurrent", "1")
	run("invocation", "policy", "set", "--agent", "builder", "--mode", "AUTOMATIC")
	run("invocation", "request", "--id", "inv-cli", "--to", "builder",
		"--instruction", "Review the CLI workflow", "--priority", "URGENT")
	run("invocation", "next", "--actor", "builder", "--runtime", "runtime-builder")
	if !bytes.Contains(out.Bytes(), []byte(`"found":true`)) ||
		!bytes.Contains(out.Bytes(), []byte(`"id":"inv-cli"`)) {
		t.Fatalf("CLI did not return the pending invocation: %s", out.String())
	}
	run("invocation", "claim", "--actor", "builder", "--id", "inv-cli", "--runtime", "runtime-builder")
	run("invocation", "start", "--actor", "builder", "--id", "inv-cli", "--summary", "started")
	run("invocation", "complete", "--actor", "builder", "--id", "inv-cli", "--summary", "done")
	run("invocation", "list", "--status", "COMPLETED")
	if !bytes.Contains(out.Bytes(), []byte(`"status":"COMPLETED"`)) {
		t.Fatalf("CLI did not return the completed invocation: %s", out.String())
	}
}
