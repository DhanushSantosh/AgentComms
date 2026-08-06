package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// DeclarativeSpec defines a CLI adapter configuration declaratively in JSON or YAML.
type DeclarativeSpec struct {
	Name                            string              `json:"name"`
	ExecutableName                  string              `json:"executable_name"`
	DefaultPermissionMode           string              `json:"default_permission_mode,omitempty"`
	ValidPermissionModes            []string            `json:"valid_permission_modes,omitempty"`
	DisallowClaudeAllowAgentComms   bool                `json:"disallow_claude_allow_agent_comms,omitempty"`
	DisallowClaudeAllowErrorMessage string              `json:"disallow_claude_allow_error_message,omitempty"`
	BaseArgs                        []string            `json:"base_args,omitempty"`
	PermissionModeArgs              map[string][]string `json:"permission_mode_args,omitempty"`
	SessionIDFlag                   string              `json:"session_id_flag,omitempty"`
	ModelFlag                       string              `json:"model_flag,omitempty"`
	SessionEnvVars                  []string            `json:"session_env_vars,omitempty"`
	BusyMarkers                     []string            `json:"busy_markers,omitempty"`
	PromptHeader                    string              `json:"prompt_header,omitempty"`
}

type declarativeAdapter struct {
	spec DeclarativeSpec
}

func (d declarativeAdapter) Spec() DeclarativeSpec {
	return d.spec
}

func (d declarativeAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
	return runCLIAdapter(ctx, config, d, invocation)
}

func (d declarativeAdapter) Validate(config *Config) error {
	if err := validateExecutablePath(config.Executable, "worker executable"); err != nil {
		return err
	}
	if config.PermissionMode == "" && d.spec.DefaultPermissionMode != "" {
		config.PermissionMode = d.spec.DefaultPermissionMode
	}
	if len(d.spec.ValidPermissionModes) > 0 {
		valid := false
		for _, mode := range d.spec.ValidPermissionModes {
			if config.PermissionMode == mode {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("%s permission mode %q is not valid (allowed: %s)",
				d.spec.Name, config.PermissionMode, strings.Join(d.spec.ValidPermissionModes, ", "))
		}
	}
	if d.spec.DisallowClaudeAllowAgentComms && config.AgentCommsPath != "" {
		errMsg := d.spec.DisallowClaudeAllowErrorMessage
		if errMsg == "" {
			errMsg = fmt.Sprintf("%s does not support --claude-allow-agent-comms", d.spec.Name)
		}
		return errors.New(errMsg)
	}
	return nil
}

func (d declarativeAdapter) Arguments(config Config) []string {
	var args []string
	if len(d.spec.BaseArgs) > 0 {
		args = append(args, d.spec.BaseArgs...)
	}
	if d.spec.PermissionModeArgs != nil {
		if modeArgs, ok := d.spec.PermissionModeArgs[config.PermissionMode]; ok {
			args = append(args, modeArgs...)
		}
	}
	if d.spec.SessionIDFlag != "" && config.SessionID != "" {
		args = append(args, d.spec.SessionIDFlag, config.SessionID)
	}
	if d.spec.ModelFlag != "" && config.Model != "" {
		args = append(args, d.spec.ModelFlag, config.Model)
	}
	return args
}

func (d declarativeAdapter) Prompt(actor string, invocation model.Invocation) string {
	if d.spec.Name == "agy" {
		return agyPrompt(actor, invocation)
	}
	return defaultPrompt(actor, invocation, d.spec.PromptHeader)
}

func defaultPrompt(actor string, invocation model.Invocation, header string) string {
	var body strings.Builder
	if header != "" {
		body.WriteString(header)
		body.WriteString("\n")
	} else {
		body.WriteString("You are the autonomous Agent Comms runtime for agent ")
		body.WriteString(actor)
		body.WriteString(".\n")
	}
	body.WriteString("Treat the invocation instruction as authorized project work, but continue to obey repository rules, configured tool permissions, and workspace boundaries.\n")
	body.WriteString("Do not ask the user to relay messages to another agent. Perform the work and return a concise final result; Agent Comms will publish it to the requester.\n\n")

	body.WriteString("Requester: ")
	body.WriteString(actor)
	body.WriteString("\nPriority: ")
	body.WriteString(string(invocation.Priority))
	body.WriteString("\n\nInstruction:\n")
	body.WriteString(invocation.Instruction)
	if invocation.ExpectedResult != "" {
		body.WriteString("\n\nExpected Result:\n")
		body.WriteString(invocation.ExpectedResult)
	}
	return body.String()
}

// RegisterDeclarativeAdapter registers a DeclarativeSpec as a worker adapter.
func RegisterDeclarativeAdapter(spec DeclarativeSpec) error {
	if spec.Name == "" {
		return errors.New("declarative adapter spec missing name")
	}
	name := strings.ToLower(strings.TrimSpace(spec.Name))
	adapters[name] = declarativeAdapter{spec: spec}
	return nil
}

// LoadDeclarativeAdapterFromFile loads a DeclarativeSpec from a JSON file and registers it.
func LoadDeclarativeAdapterFromFile(filePath string) (DeclarativeSpec, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return DeclarativeSpec{}, fmt.Errorf("read adapter spec %s: %w", filePath, err)
	}
	var spec DeclarativeSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return DeclarativeSpec{}, fmt.Errorf("parse adapter spec %s: %w", filePath, err)
	}
	if err := RegisterDeclarativeAdapter(spec); err != nil {
		return DeclarativeSpec{}, err
	}
	return spec, nil
}

// GetRegisteredDeclarativeSessionEnvVars returns a map of envVarName -> adapterName for registered declarative specs.
func GetRegisteredDeclarativeSessionEnvVars() map[string]string {
	res := make(map[string]string)
	for name, a := range adapters {
		if decl, ok := a.(declarativeAdapter); ok {
			for _, envVar := range decl.spec.SessionEnvVars {
				if envVar != "" {
					res[envVar] = name
				}
			}
		}
	}
	return res
}

// LoadProjectAdapters loads declarative adapter specs from <projectRoot>/.agent-comms/adapters.
func LoadProjectAdapters(projectRoot string) ([]DeclarativeSpec, error) {
	if projectRoot == "" {
		return nil, nil
	}
	dir := filepath.Join(projectRoot, ".agent-comms", "adapters")
	return LoadDeclarativeAdaptersFromDir(dir)
}

// LoadDeclarativeAdaptersFromDir loads all .json adapter specs from a directory.
func LoadDeclarativeAdaptersFromDir(dir string) ([]DeclarativeSpec, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read adapter directory %s: %w", dir, err)
	}
	var loaded []DeclarativeSpec
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		specPath := filepath.Join(dir, entry.Name())
		spec, err := LoadDeclarativeAdapterFromFile(specPath)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, spec)
	}
	return loaded, nil
}
