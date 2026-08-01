package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DhanushSantosh/AgentComms/internal/app"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/failure"
	"github.com/DhanushSantosh/AgentComms/internal/mcp"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
)

const referencePath = "sites/docs/src/generated/reference.json"

type errorDocument struct {
	Code        string `json:"code"`
	ExitStatus  int    `json:"exit_status"`
	Description string `json:"description"`
}

type referenceDocument struct {
	SchemaVersion   string                `json:"schema_version"`
	ProtocolVersion string                `json:"protocol_version"`
	MCPVersion      string                `json:"mcp_version"`
	Commands        []app.CommandDocument `json:"commands"`
	MCPTools        []map[string]any      `json:"mcp_tools"`
	ErrorCodes      []errorDocument       `json:"error_codes"`
}

func main() {
	check := flag.Bool("check", false, "fail when the committed reference differs from the source")
	output := flag.String("output", referencePath, "reference output path, relative to the repository root")
	flag.Parse()

	root, err := repositoryRoot()
	if err != nil {
		fatal(err)
	}
	body, err := json.MarshalIndent(buildReference(), "", "  ")
	if err != nil {
		fatal(fmt.Errorf("encode reference: %w", err))
	}
	body = append(body, '\n')
	path := filepath.Join(root, filepath.FromSlash(*output))
	if *check {
		committed, readErr := os.ReadFile(path)
		if readErr != nil {
			fatal(fmt.Errorf("read generated reference: %w", readErr))
		}
		if !bytes.Equal(committed, body) {
			fatal(fmt.Errorf("generated documentation reference is stale; run `npm run docs:generate`"))
		}
		return
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fatal(fmt.Errorf("create reference directory: %w", err))
	}
	if err = os.WriteFile(path, body, 0o644); err != nil {
		fatal(fmt.Errorf("write generated reference: %w", err))
	}
}

func buildReference() referenceDocument {
	return referenceDocument{
		SchemaVersion: model.SchemaVersion, ProtocolVersion: app.APIVersion,
		MCPVersion: mcp.ProtocolVersion, Commands: app.CommandDocumentation(),
		MCPTools: mcp.ToolDocumentation(), ErrorCodes: documentedErrors(),
	}
}

func documentedErrors() []errorDocument {
	return []errorDocument{
		{Code: string(controlplane.CodeValidation), ExitStatus: 2, Description: "The command or payload is invalid."},
		{Code: string(controlplane.CodeAuthorization), ExitStatus: 3, Description: "The actor does not have the required identity, role, scope, or human authority."},
		{Code: string(controlplane.CodeIntegrity), ExitStatus: 5, Description: "A signature, hash, receipt, or event-chain check failed."},
		{Code: failure.CodeExternal, ExitStatus: 7, Description: "An external connector, process, or remote dependency failed."},
		{Code: string(controlplane.CodeOffline), ExitStatus: 8, Description: "The authoritative service is offline; governed writes cannot proceed."},
		{Code: string(controlplane.CodeUnavailable), ExitStatus: 8, Description: "The requested runtime, delivery path, or service is unavailable."},
		{Code: string(controlplane.CodeConflict), ExitStatus: 9, Description: "The mutation conflicts with current governed state."},
		{Code: string(controlplane.CodeStalePrecondition), ExitStatus: 9, Description: "State changed after the caller's precondition was observed."},
		{Code: string(controlplane.CodeRateLimited), ExitStatus: 10, Description: "Admission or rate limits rejected the request; retry according to the response."},
		{Code: string(projectlifecycle.CodeUpgradeRequired), ExitStatus: 11, Description: "The project requires an explicit managed upgrade."},
		{Code: string(projectlifecycle.CodeProjectTooNew), ExitStatus: 11, Description: "The project was written by a newer incompatible binary."},
		{Code: string(projectlifecycle.CodeUpgradeUnsupported), ExitStatus: 11, Description: "This binary cannot perform the required project upgrade."},
		{Code: string(projectlifecycle.CodeUpgradeFailed), ExitStatus: 12, Description: "A managed project upgrade did not complete."},
	}
}

func repositoryRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(directory, "go.mod")); statErr == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("could not find repository root from %s", directory)
		}
		directory = parent
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
