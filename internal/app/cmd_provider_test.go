package app

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newProviderProject initializes a personal project owned by "owner".
func newProviderProject(t *testing.T) string {
	t.Helper()
	project := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	cleanupProjectDaemon(t, project)
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"init", "--project", project, "--non-interactive", "--owner", "owner", "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("init: %v\n%s", err, stderr.String())
	}
	return project
}

func runProviderCLI(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	c := &cli{out: &stdout, err: &stderr, timeout: 10 * time.Second, in: strings.NewReader(input)}
	root := c.root()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String() + stderr.String(), err
}

func TestProviderLifecycleThroughTheCLI(t *testing.T) {
	project := newProviderProject(t)

	out, err := runProviderCLI(t, "", "agent", "register", "--project", project, "--provider", "gemini", "--non-interactive")
	if err == nil || !strings.Contains(err.Error(), "agent-comms provider add gemini") {
		t.Fatalf("an unregistered provider must name the exact command, got %v\n%s", err, out)
	}

	if out, err = runProviderCLI(t, "", "provider", "add", "gemini", "--project", project, "--display-name", "Google Gemini CLI", "--output", "plain"); err != nil {
		t.Fatalf("provider add: %v\n%s", err, out)
	}
	if out, err = runProviderCLI(t, "", "agent", "register", "--project", project, "--provider", "gemini", "--output", "plain"); err != nil || !strings.Contains(out, "gemini") {
		t.Fatalf("agent register --provider gemini: %v\n%s", err, out)
	}
	if out, err = runProviderCLI(t, "", "agent", "register", "--project", project, "--id", "gemini-reviewer", "--output", "plain"); err != nil {
		t.Fatalf("agent register --id gemini-reviewer: %v\n%s", err, out)
	}

	out, err = runProviderCLI(t, "", "provider", "list", "--project", project, "--json")
	if err != nil {
		t.Fatalf("provider list: %v\n%s", err, out)
	}
	var listing struct {
		Result map[string]struct {
			Status      string `json:"status"`
			BuiltIn     bool   `json:"built_in"`
			DisplayName string `json:"display_name"`
			AddedBy     string `json:"added_by"`
		} `json:"result"`
		Order []string `json:"order"`
	}
	if err = json.Unmarshal([]byte(out), &listing); err != nil {
		t.Fatalf("provider list JSON: %v\n%s", err, out)
	}
	if got := strings.Join(listing.Order, ","); got != "claude,codex,opencode,gemini" {
		t.Fatalf("order = %s", got)
	}
	if gemini := listing.Result["gemini"]; gemini.BuiltIn || gemini.Status != "ACTIVE" || gemini.DisplayName != "Google Gemini CLI" || gemini.AddedBy != "owner" {
		t.Fatalf("gemini listing = %+v", gemini)
	}

	if out, err = runProviderCLI(t, "", "provider", "retire", "gemini", "--project", project, "--reason", "trial ended", "--output", "plain"); err != nil {
		t.Fatalf("provider retire: %v\n%s", err, out)
	}
	if _, err = runProviderCLI(t, "", "agent", "register", "--project", project, "--id", "gemini-3", "--non-interactive"); err == nil ||
		!strings.Contains(err.Error(), "reactivate it") {
		t.Fatalf("a retired provider must refuse new agents, got %v", err)
	}
	if out, err = runProviderCLI(t, "", "provider", "show", "gemini", "--project", project, "--output", "plain"); err != nil ||
		!strings.Contains(out, "RETIRED") || !strings.Contains(out, "trial ended") {
		t.Fatalf("provider show: %v\n%s", err, out)
	}
	if _, err = runProviderCLI(t, "", "provider", "retire", "codex", "--project", project, "--reason", "x"); err == nil {
		t.Fatal("a built-in provider must not be retirable")
	}
}

func TestAgentRegisterOffersToRegisterAMissingProvider(t *testing.T) {
	project := newProviderProject(t)

	out, err := runProviderCLI(t, "n\n", "agent", "register", "--project", project, "--provider", "aider")
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("answering n must cancel, got %v\n%s", err, out)
	}
	if !strings.Contains(out, `Provider "aider" is not registered for this project. Register it now? [y/N]`) {
		t.Fatalf("missing prompt:\n%s", out)
	}

	if out, err = runProviderCLI(t, "y\n", "agent", "register", "--project", project, "--id", "aider-main", "--output", "plain"); err != nil {
		t.Fatalf("answering y must register the provider and the agent: %v\n%s", err, out)
	}
	out, err = runProviderCLI(t, "", "history", "--project", project, "--output", "plain")
	if err != nil {
		t.Fatal(err)
	}
	register := strings.Index(out, "provider.register")
	agent := strings.LastIndex(out, "agent.register")
	if register < 0 || agent < register {
		t.Fatalf("expected provider.register before the agent.register:\n%s", out)
	}

	// --json never prompts.
	if _, err = runProviderCLI(t, "y\n", "agent", "register", "--project", project, "--provider", "kilo", "--json"); err == nil {
		t.Fatal("--json must not prompt or register a provider implicitly")
	}
}
