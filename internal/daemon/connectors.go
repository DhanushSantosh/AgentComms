package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/google/uuid"
)

const (
	defaultConnectorTimeout = 30 * time.Second
	maxConnectorConfigBytes = 256 * 1024
	maxConnectorArgs        = 64
	maxConnectorArgBytes    = 4 * 1024
	baseDeliveryBackoff     = 2 * time.Second
	maxDeliveryBackoff      = 5 * time.Minute
)

type ConnectorConfig struct {
	Type             string            `json:"type"`
	Executable       string            `json:"executable,omitempty"`
	Arguments        []string          `json:"arguments,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	Environment      map[string]string `json:"environment,omitempty"`
	Timeout          time.Duration     `json:"-"`
}

type connectorConfigFile struct {
	Connectors map[string]connectorConfigJSON `json:"connectors"`
}

type connectorConfigJSON struct {
	Type             string            `json:"type"`
	Executable       string            `json:"executable,omitempty"`
	Arguments        []string          `json:"arguments,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	Environment      map[string]string `json:"environment,omitempty"`
	Timeout          string            `json:"timeout,omitempty"`
}

type InvocationEnvelope struct {
	ProjectID  string             `json:"project_id"`
	Invocation model.Invocation   `json:"invocation"`
	Runtime    model.AgentRuntime `json:"runtime"`
}

type commandSubmitter func(context.Context, string, string, string, string, any) error

type Dispatcher struct {
	configs map[string]ConnectorConfig
	submit  commandSubmitter
	now     func() time.Time
	launch  func(context.Context, ConnectorConfig, InvocationEnvelope) error
}

func LoadConnectorConfigs(path string) (map[string]ConnectorConfig, error) {
	if strings.TrimSpace(path) == "" {
		return map[string]ConnectorConfig{}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect connector config: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("connector config must not be accessible by group or other users")
	}
	if info.Size() > maxConnectorConfigBytes {
		return nil, errors.New("connector config exceeds the size limit")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var source connectorConfigFile
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&source); err != nil {
		return nil, fmt.Errorf("decode connector config: %w", err)
	}
	configs := make(map[string]ConnectorConfig, len(source.Connectors))
	for reference, value := range source.Connectors {
		timeout := defaultConnectorTimeout
		if value.Timeout != "" {
			timeout, err = time.ParseDuration(value.Timeout)
			if err != nil || timeout <= 0 || timeout > controlplane.DefaultRequestTimeout*4 {
				return nil, fmt.Errorf("connector %s has an invalid timeout", reference)
			}
		}
		config := ConnectorConfig{
			Type: strings.ToUpper(value.Type), Executable: value.Executable,
			Arguments: value.Arguments, WorkingDirectory: value.WorkingDirectory,
			Environment: value.Environment, Timeout: timeout,
		}
		if err = validateConnectorConfig(reference, config); err != nil {
			return nil, err
		}
		configs[reference] = config
	}
	return configs, nil
}

func validateConnectorConfig(reference string, config ConnectorConfig) error {
	if strings.TrimSpace(reference) == "" {
		return errors.New("connector configuration reference is required")
	}
	if config.Type != "MANUAL" && config.Type != "MCP" && config.Type != "LOCAL_PROCESS" {
		return fmt.Errorf("connector %s type must be MANUAL, MCP, or LOCAL_PROCESS", reference)
	}
	if config.Type != "LOCAL_PROCESS" {
		return nil
	}
	if !filepath.IsAbs(config.Executable) {
		return fmt.Errorf("connector %s executable must be an absolute path", reference)
	}
	if config.WorkingDirectory != "" && !filepath.IsAbs(config.WorkingDirectory) {
		return fmt.Errorf("connector %s working directory must be absolute", reference)
	}
	if len(config.Arguments) > maxConnectorArgs {
		return fmt.Errorf("connector %s has too many arguments", reference)
	}
	for _, argument := range config.Arguments {
		if len(argument) > maxConnectorArgBytes {
			return fmt.Errorf("connector %s argument exceeds the size limit", reference)
		}
	}
	for key := range config.Environment {
		if strings.TrimSpace(key) == "" || strings.Contains(key, "=") ||
			strings.HasPrefix(key, "AGENT_COMMS_") {
			return fmt.Errorf("connector %s environment key %q is reserved or invalid", reference, key)
		}
	}
	return nil
}

func NewDispatcher(configs map[string]ConnectorConfig, submit commandSubmitter) (*Dispatcher, error) {
	if submit == nil {
		return nil, errors.New("connector command submitter is required")
	}
	if configs == nil {
		configs = map[string]ConnectorConfig{}
	}
	for reference, config := range configs {
		if config.Timeout <= 0 {
			config.Timeout = defaultConnectorTimeout
			configs[reference] = config
		}
		if err := validateConnectorConfig(reference, config); err != nil {
			return nil, err
		}
	}
	return &Dispatcher{
		configs: configs, submit: submit, now: func() time.Time { return time.Now().UTC() },
		launch: launchConnector,
	}, nil
}

func (d *Dispatcher) Dispatch(ctx context.Context, projectID string, state model.State) error {
	invocationIDs := make([]string, 0, len(state.Invocations))
	for id := range state.Invocations {
		invocationIDs = append(invocationIDs, id)
	}
	sort.Strings(invocationIDs)
	for _, invocationID := range invocationIDs {
		invocation := state.Invocations[invocationID]
		if invocation.Status != "PENDING" ||
			(invocation.NextAttemptAt != nil && invocation.NextAttemptAt.After(d.now())) {
			continue
		}
		runtime, config, found := d.selectRuntime(state, invocation)
		if !found {
			continue
		}
		attempt := deliveryAttempt(state, invocation.ID) + 1
		if attempt > controlplane.MaxDeliveryAttempts {
			continue
		}
		deliveryID := uuid.NewString()
		if err := d.submit(ctx, projectID, runtime.AgentID, "invocation.notify", invocation.ID,
			model.InvocationNotified{DeliveryID: deliveryID, RuntimeID: runtime.ID, Attempt: attempt}); err != nil {
			continue
		}
		envelope := InvocationEnvelope{ProjectID: projectID, Invocation: invocation, Runtime: runtime}
		if err := d.launch(ctx, config, envelope); err != nil {
			final := attempt == controlplane.MaxDeliveryAttempts
			var retryAt *time.Time
			if !final {
				next := d.now().Add(deliveryBackoff(attempt))
				retryAt = &next
			}
			if submitErr := d.submit(ctx, projectID, runtime.AgentID, "invocation.delivery-failed", invocation.ID,
				model.InvocationDeliveryFailed{
					DeliveryID: deliveryID, RuntimeID: runtime.ID, Attempt: attempt,
					Error: err.Error(), NextRetry: retryAt, Final: final,
				}); submitErr != nil {
				return submitErr
			}
		}
	}
	return nil
}

func (d *Dispatcher) selectRuntime(state model.State, invocation model.Invocation) (model.AgentRuntime, ConnectorConfig, bool) {
	runtimeIDs := make([]string, 0, len(state.AgentRuntimes))
	for id := range state.AgentRuntimes {
		runtimeIDs = append(runtimeIDs, id)
	}
	sort.Strings(runtimeIDs)
	for _, runtimeID := range runtimeIDs {
		runtime := state.AgentRuntimes[runtimeID]
		if runtime.AgentID != invocation.Target || runtime.Status == "DRAINING" ||
			runtime.Status == "REVOKED" || len(runtime.ActiveInvocations) >= runtime.MaxConcurrent {
			continue
		}
		config := ConnectorConfig{Type: runtime.Connector, Timeout: defaultConnectorTimeout}
		if runtime.ConfigReference != "" {
			var configured bool
			config, configured = d.configs[runtime.ConfigReference]
			if !configured {
				continue
			}
		}
		if config.Type != runtime.Connector {
			continue
		}
		if config.Type == "MCP" && runtime.Status != "ONLINE" {
			continue
		}
		if config.Type == "WEBHOOK" || config.Type == "QUEUE" {
			continue
		}
		return runtime, config, true
	}
	return model.AgentRuntime{}, ConnectorConfig{}, false
}

func launchConnector(ctx context.Context, config ConnectorConfig, envelope InvocationEnvelope) error {
	if config.Type == "MANUAL" || config.Type == "MCP" {
		return nil
	}
	if config.Type != "LOCAL_PROCESS" {
		return fmt.Errorf("unsupported connector type %s", config.Type)
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultConnectorTimeout
	}
	processCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	arguments := make([]string, len(config.Arguments))
	for index, argument := range config.Arguments {
		arguments[index] = replaceConnectorVariables(argument, envelope)
	}
	command := exec.CommandContext(processCtx, config.Executable, arguments...)
	command.Dir = config.WorkingDirectory
	command.Env = append(os.Environ(),
		"AGENT_COMMS_PROJECT_ID="+envelope.ProjectID,
		"AGENT_COMMS_INVOCATION_ID="+envelope.Invocation.ID,
		"AGENT_COMMS_AGENT_ID="+envelope.Runtime.AgentID,
		"AGENT_COMMS_RUNTIME_ID="+envelope.Runtime.ID,
	)
	for key, value := range config.Environment {
		if strings.Contains(key, "=") || strings.TrimSpace(key) == "" ||
			strings.HasPrefix(key, "AGENT_COMMS_") {
			return errors.New("connector environment key is invalid")
		}
		command.Env = append(command.Env, key+"="+value)
	}
	command.Stdin = strings.NewReader(string(payload))
	return command.Run()
}

func replaceConnectorVariables(value string, envelope InvocationEnvelope) string {
	replacer := strings.NewReplacer(
		"{project_id}", envelope.ProjectID,
		"{invocation_id}", envelope.Invocation.ID,
		"{agent_id}", envelope.Runtime.AgentID,
		"{runtime_id}", envelope.Runtime.ID,
	)
	return replacer.Replace(value)
}

func deliveryAttempt(state model.State, invocationID string) int {
	maxAttempt := 0
	for _, delivery := range state.InvocationDeliveries {
		if delivery.InvocationID == invocationID && delivery.Attempt > maxAttempt {
			maxAttempt = delivery.Attempt
		}
	}
	return maxAttempt
}

func deliveryBackoff(attempt int) time.Duration {
	backoff := baseDeliveryBackoff * time.Duration(1<<min(attempt-1, 8))
	return min(backoff, maxDeliveryBackoff)
}
