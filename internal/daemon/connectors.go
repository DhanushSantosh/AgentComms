package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
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
	maxConnectorHeaders     = 32
	maxConnectorHeaderBytes = 8 * 1024
	maxConnectorResponse    = 64 * 1024
	baseDeliveryBackoff     = 2 * time.Second
	maxDeliveryBackoff      = 5 * time.Minute
)

type ConnectorConfig struct {
	Type             string            `json:"type"`
	Executable       string            `json:"executable,omitempty"`
	Arguments        []string          `json:"arguments,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	Environment      map[string]string `json:"environment,omitempty"`
	Endpoint         string            `json:"endpoint,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
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
	Endpoint         string            `json:"endpoint,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	Timeout          string            `json:"timeout,omitempty"`
}

type InvocationEnvelope struct {
	ProjectID  string             `json:"project_id"`
	Invocation model.Invocation   `json:"invocation"`
	Runtime    model.AgentRuntime `json:"runtime"`
}

type commandSubmitter func(context.Context, string, string, string, string, any) error

type Dispatcher struct {
	configs     map[string]ConnectorConfig
	configPath  string
	configMu    sync.RWMutex
	submit      commandSubmitter
	now         func() time.Time
	projectRoot string
	hostID      string
	launch      func(context.Context, ConnectorConfig, InvocationEnvelope) error
	deliver     func(context.Context, ConnectorConfig, InvocationEnvelope) (DeliveryResult, error)
}

type DeliveryResult struct {
	EndpointID string
	Evidence   []model.DeliveryEvidence
}

func LoadConnectorConfigs(path string) (map[string]ConnectorConfig, error) {
	if strings.TrimSpace(path) == "" {
		return map[string]ConnectorConfig{}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect connector config: %w", err)
	}
	if !connectorConfigPermissionsPrivate(info) {
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
			Environment: value.Environment, Endpoint: value.Endpoint, Headers: value.Headers,
			Timeout: timeout,
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
	if config.Type != "MANUAL" && config.Type != "MCP" &&
		config.Type != "LOCAL_PROCESS" && config.Type != "WEBHOOK" {
		return fmt.Errorf("connector %s type must be MANUAL, MCP, LOCAL_PROCESS, or WEBHOOK", reference)
	}
	if config.Type == "WEBHOOK" {
		return validateWebhookConfig(reference, config)
	}
	if config.Type != "LOCAL_PROCESS" {
		return nil
	}
	if !filepath.IsAbs(config.Executable) {
		return fmt.Errorf("connector %s executable must be an absolute path", reference)
	}
	executableInfo, err := os.Stat(config.Executable)
	if err != nil {
		return fmt.Errorf("connector %s executable is unavailable: %w", reference, err)
	}
	if !executableInfo.Mode().IsRegular() {
		return fmt.Errorf("connector %s executable must be a regular file", reference)
	}
	if runtime.GOOS != "windows" && executableInfo.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("connector %s executable is not executable", reference)
	}
	if runtime.GOOS != "windows" && executableInfo.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("connector %s executable must not be writable by group or other users", reference)
	}
	if config.WorkingDirectory != "" && !filepath.IsAbs(config.WorkingDirectory) {
		return fmt.Errorf("connector %s working directory must be absolute", reference)
	}
	if config.WorkingDirectory != "" {
		workingDirectoryInfo, err := os.Stat(config.WorkingDirectory)
		if err != nil || !workingDirectoryInfo.IsDir() {
			return fmt.Errorf("connector %s working directory must be an existing directory", reference)
		}
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

func validateWebhookConfig(reference string, config ConnectorConfig) error {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Host == "" || endpoint.User != nil {
		return fmt.Errorf("connector %s webhook endpoint is invalid", reference)
	}
	if endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && isLoopbackHost(endpoint.Hostname())) {
		return fmt.Errorf("connector %s webhook endpoint must use HTTPS or loopback HTTP", reference)
	}
	if len(config.Headers) > maxConnectorHeaders {
		return fmt.Errorf("connector %s has too many webhook headers", reference)
	}
	headerBytes := 0
	for key, value := range config.Headers {
		headerBytes += len(key) + len(value)
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n:") ||
			strings.ContainsAny(value, "\r\n") || strings.EqualFold(key, "Host") ||
			strings.EqualFold(key, "Content-Length") {
			return fmt.Errorf("connector %s webhook header %q is invalid", reference, key)
		}
	}
	if headerBytes > maxConnectorHeaderBytes {
		return fmt.Errorf("connector %s webhook headers exceed the size limit", reference)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
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
	dispatcher := &Dispatcher{
		configs: configs, submit: submit, now: func() time.Time { return time.Now().UTC() },
		launch: launchConnector,
	}
	dispatcher.deliver = dispatcher.deliverConfiguredConnector
	return dispatcher, nil
}

func (d *Dispatcher) SetLocalInteractive(projectRoot, hostID string) {
	d.projectRoot = projectRoot
	d.hostID = hostID
	d.deliver = func(ctx context.Context, config ConnectorConfig, envelope InvocationEnvelope) (DeliveryResult, error) {
		if config.Type != "INTERACTIVE" {
			return d.deliverConfiguredConnector(ctx, config, envelope)
		}
		evidence, err := notifyLocalInteractive(
			ctx, projectRoot, envelope.Runtime.ID, envelope.Runtime.AgentID,
			envelope.Invocation.ID, envelope.Invocation.RequestedBy,
		)
		if err != nil {
			return DeliveryResult{}, err
		}
		return DeliveryResult{EndpointID: envelope.Runtime.EndpointID, Evidence: evidence}, nil
	}
}

func (d *Dispatcher) SetConfigSource(path string) {
	d.configPath = strings.TrimSpace(path)
}

func (d *Dispatcher) reloadConfigs() error {
	if d.configPath == "" {
		return nil
	}
	configs, err := LoadConnectorConfigs(d.configPath)
	if err != nil {
		return err
	}
	d.configMu.Lock()
	d.configs = configs
	d.configMu.Unlock()
	return nil
}

func (d *Dispatcher) connectorConfig(reference string) (ConnectorConfig, bool) {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	config, exists := d.configs[reference]
	return config, exists
}

func (d *Dispatcher) ValidateRuntime(connector, configReference, hostID string) error {
	if err := d.reloadConfigs(); err != nil {
		return err
	}
	connector = strings.ToUpper(strings.TrimSpace(connector))
	if connector == "INTERACTIVE" {
		if d.hostID == "" || hostID != d.hostID {
			return errors.New("interactive runtime host ID does not match this local daemon")
		}
		return nil
	}
	if connector != "LOCAL_PROCESS" && connector != "WEBHOOK" {
		return nil
	}
	config, exists := d.connectorConfig(configReference)
	if !exists {
		return fmt.Errorf("connector configuration reference %q is not available on this host", configReference)
	}
	if config.Type != connector {
		return fmt.Errorf("connector configuration %q has type %s, expected %s", configReference, config.Type, connector)
	}
	return validateConnectorConfig(configReference, config)
}

func (d *Dispatcher) Dispatch(ctx context.Context, projectID string, state model.State) error {
	if err := d.reloadConfigs(); err != nil {
		return fmt.Errorf("reload connector configuration: %w", err)
	}
	if err := d.expireAttempts(ctx, projectID, state); err != nil {
		return err
	}
	invocationIDs := make([]string, 0, len(state.Invocations))
	for id := range state.Invocations {
		invocationIDs = append(invocationIDs, id)
	}
	sort.Strings(invocationIDs)
	for _, invocationID := range invocationIDs {
		invocation := state.Invocations[invocationID]
		if invocation.Status != "PENDING" || deliveryRetryPending(state, invocation.ID, d.now()) {
			continue
		}
		runtime, config, found := d.selectRuntime(state, invocation)
		if !found {
			continue
		}
		attempt := deliveryAttempt(state, invocation.ID) + 1
		if automaticDeliveryAttemptCount(state, invocation.ID) >= controlplane.MaxDeliveryAttempts {
			continue
		}
		deliveryID := uuid.NewString()
		if err := d.submit(ctx, projectID, runtime.AgentID, "invocation.delivery-attempt", invocation.ID,
			model.InvocationDeliveryAttempted{
				DeliveryID: deliveryID, RuntimeID: runtime.ID,
				Transport: runtime.Connector, HostID: runtime.HostID,
				EndpointID: runtime.EndpointID,
			}); err != nil {
			continue
		}
		envelope := InvocationEnvelope{ProjectID: projectID, Invocation: invocation, Runtime: runtime}
		result, deliveryErr := d.deliver(ctx, config, envelope)
		if deliveryErr != nil {
			final := automaticDeliveryAttemptCount(state, invocation.ID)+1 >= controlplane.MaxDeliveryAttempts
			var retryAt *time.Time
			if !final {
				next := d.now().Add(deliveryBackoff(attempt))
				retryAt = &next
			}
			if submitErr := d.submit(ctx, projectID, runtime.AgentID, "invocation.delivery-failed", invocation.ID,
				model.InvocationDeliveryFailed{
					DeliveryID: deliveryID, RuntimeID: runtime.ID, Attempt: attempt,
					Error: deliveryErr.Error(), NextRetry: retryAt, Final: final,
				}); submitErr != nil {
				return submitErr
			}
			continue
		}
		if err := d.submit(ctx, projectID, runtime.AgentID, "invocation.notify", invocation.ID,
			model.InvocationNotified{
				DeliveryID: deliveryID, RuntimeID: runtime.ID, Attempt: attempt,
				Transport: runtime.Connector, EndpointID: result.EndpointID,
				Evidence: result.Evidence,
			}); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dispatcher) DeliverAttempt(ctx context.Context, projectID, invocationID, deliveryID string, state model.State) error {
	delivery, exists := state.InvocationDeliveries[deliveryID]
	if !exists || delivery.InvocationID != invocationID || delivery.Status != "ATTEMPTED" {
		return errors.New("matching attempted delivery is required")
	}
	if delivery.AttemptUntil == nil || !delivery.AttemptUntil.After(d.now()) {
		return errors.New("delivery attempt lease has expired")
	}
	invocation, exists := state.Invocations[invocationID]
	if !exists {
		return errors.New("invocation not found")
	}
	runtimeState, exists := state.AgentRuntimes[delivery.RuntimeID]
	if !exists {
		return errors.New("delivery runtime not found")
	}
	config, err := d.configForRuntime(runtimeState)
	if err != nil {
		return d.recordDeliveryFailure(ctx, projectID, invocation, delivery, state, err)
	}
	result, deliveryErr := d.deliver(ctx, config, InvocationEnvelope{
		ProjectID: projectID, Invocation: invocation, Runtime: runtimeState,
	})
	if deliveryErr != nil {
		return d.recordDeliveryFailure(ctx, projectID, invocation, delivery, state, deliveryErr)
	}
	return d.submit(ctx, projectID, runtimeState.AgentID, "invocation.notify", invocationID,
		model.InvocationNotified{
			DeliveryID: delivery.ID, RuntimeID: runtimeState.ID, Attempt: delivery.Attempt,
			Transport: runtimeState.Connector, EndpointID: result.EndpointID,
			Evidence: result.Evidence,
		})
}

func (d *Dispatcher) configForRuntime(runtimeState model.AgentRuntime) (ConnectorConfig, error) {
	if err := d.reloadConfigs(); err != nil {
		return ConnectorConfig{}, fmt.Errorf("reload connector configuration: %w", err)
	}
	if effectiveRuntimeKind(runtimeState) == model.RuntimeKindInteractive {
		if runtimeState.Connector != "INTERACTIVE" || runtimeState.Status != "ONLINE" ||
			runtimeState.HostID == "" || runtimeState.HostID != d.hostID ||
			d.projectRoot == "" {
			return ConnectorConfig{}, errors.New("interactive runtime is not available on this host")
		}
		return ConnectorConfig{Type: "INTERACTIVE", Timeout: defaultConnectorTimeout}, nil
	}
	if runtimeState.Connector == "MANUAL" || runtimeState.Connector == "MCP" ||
		runtimeState.Connector == "QUEUE" {
		return ConnectorConfig{}, fmt.Errorf("%s runtime does not support notification delivery", runtimeState.Connector)
	}
	if runtimeState.ConfigReference == "" {
		return ConnectorConfig{}, errors.New("runtime connector configuration reference is missing")
	}
	config, exists := d.connectorConfig(runtimeState.ConfigReference)
	if !exists || config.Type != runtimeState.Connector {
		return ConnectorConfig{}, errors.New("runtime connector configuration is unavailable or mismatched")
	}
	if err := validateConnectorConfig(runtimeState.ConfigReference, config); err != nil {
		return ConnectorConfig{}, err
	}
	return config, nil
}

func (d *Dispatcher) recordDeliveryFailure(
	ctx context.Context,
	projectID string,
	invocation model.Invocation,
	delivery model.InvocationDelivery,
	state model.State,
	deliveryErr error,
) error {
	final := delivery.Manual ||
		automaticDeliveryAttemptCount(state, invocation.ID) >= controlplane.MaxDeliveryAttempts
	var retryAt *time.Time
	if !final {
		next := d.now().Add(deliveryBackoff(delivery.Attempt))
		retryAt = &next
	}
	return d.submit(ctx, projectID, invocation.Target, "invocation.delivery-failed", invocation.ID,
		model.InvocationDeliveryFailed{
			DeliveryID: delivery.ID, RuntimeID: delivery.RuntimeID,
			Attempt: delivery.Attempt, Error: deliveryErr.Error(),
			NextRetry: retryAt, Final: final,
		})
}

func (d *Dispatcher) expireAttempts(ctx context.Context, projectID string, state model.State) error {
	now := d.now()
	for _, delivery := range state.InvocationDeliveries {
		if delivery.Status != "ATTEMPTED" || delivery.AttemptUntil == nil ||
			delivery.AttemptUntil.After(now) {
			continue
		}
		invocation, exists := state.Invocations[delivery.InvocationID]
		if !exists {
			continue
		}
		final := !delivery.Manual &&
			automaticDeliveryAttemptCount(state, delivery.InvocationID) >= controlplane.MaxDeliveryAttempts
		var retryAt *time.Time
		if !final {
			next := now.Add(deliveryBackoff(delivery.Attempt))
			retryAt = &next
		}
		if err := d.submit(ctx, projectID, invocation.Target, "invocation.delivery-failed", delivery.InvocationID,
			model.InvocationDeliveryFailed{
				DeliveryID: delivery.ID, RuntimeID: delivery.RuntimeID,
				Attempt: delivery.Attempt, Error: "delivery attempt lease expired",
				NextRetry: retryAt, Final: final,
			}); err != nil {
			return err
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
	type candidate struct {
		runtime model.AgentRuntime
		config  ConnectorConfig
	}
	candidates := make([]candidate, 0, len(runtimeIDs))
	interactiveCandidates := 0
	for _, runtimeID := range runtimeIDs {
		runtime := state.AgentRuntimes[runtimeID]
		if runtime.AgentID != invocation.Target || runtime.Status == "DRAINING" ||
			runtime.Status == "REVOKED" || len(runtime.ActiveInvocations) >= runtime.MaxConcurrent ||
			(invocation.PreferredRuntimeID != "" && invocation.PreferredRuntimeID != runtime.ID) {
			continue
		}
		kind := effectiveRuntimeKind(runtime)
		if !consumerModeAllowsRuntime(invocation.ConsumerMode, kind) {
			continue
		}
		if kind == model.RuntimeKindInteractive {
			if runtime.Status != "ONLINE" || runtime.HostID == "" ||
				d.hostID == "" || runtime.HostID != d.hostID ||
				runtime.Connector != "INTERACTIVE" || d.projectRoot == "" {
				continue
			}
			interactiveCandidates++
			candidates = append(candidates, candidate{
				runtime: runtime,
				config:  ConnectorConfig{Type: "INTERACTIVE", Timeout: defaultConnectorTimeout},
			})
			continue
		}
		if runtime.Connector == "MANUAL" || runtime.Connector == "MCP" ||
			runtime.Connector == "QUEUE" || runtime.ConfigReference == "" {
			continue
		}
		config, configured := d.connectorConfig(runtime.ConfigReference)
		if !configured || config.Type != runtime.Connector {
			continue
		}
		candidates = append(candidates, candidate{runtime: runtime, config: config})
	}
	if invocation.PreferredRuntimeID == "" &&
		effectiveConsumerMode(invocation.ConsumerMode) == model.ConsumerModeInteractiveOnly &&
		interactiveCandidates > 1 {
		return model.AgentRuntime{}, ConnectorConfig{}, false
	}
	for _, item := range candidates {
		if interactiveCandidates > 1 && invocation.PreferredRuntimeID == "" &&
			effectiveRuntimeKind(item.runtime) == model.RuntimeKindInteractive {
			continue
		}
		return item.runtime, item.config, true
	}
	return model.AgentRuntime{}, ConnectorConfig{}, false
}

func (d *Dispatcher) deliverConfiguredConnector(ctx context.Context, config ConnectorConfig, envelope InvocationEnvelope) (DeliveryResult, error) {
	if config.Type == "INTERACTIVE" {
		return DeliveryResult{}, errors.New("interactive delivery is not configured")
	}
	if err := d.launch(ctx, config, envelope); err != nil {
		return DeliveryResult{}, err
	}
	acceptedAt := time.Now().UTC()
	return DeliveryResult{Evidence: []model.DeliveryEvidence{{
		Stage: "CONNECTOR_ACCEPTED", At: acceptedAt,
	}}}, nil
}

func launchConnector(ctx context.Context, config ConnectorConfig, envelope InvocationEnvelope) error {
	if config.Type == "MANUAL" || config.Type == "MCP" {
		return fmt.Errorf("%s connector cannot prove notification delivery", config.Type)
	}
	if config.Type == "WEBHOOK" {
		return launchWebhook(ctx, config, envelope)
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

func launchWebhook(ctx context.Context, config ConnectorConfig, envelope InvocationEnvelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultConnectorTimeout
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, config.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "agent-comms-runtime-relay")
	for key, value := range config.Headers {
		request.Header.Set(key, value)
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("webhook redirects are disabled")
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, maxConnectorResponse))
	if readErr != nil {
		return readErr
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("webhook returned %s", response.Status)
	}
	return nil
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

func automaticDeliveryAttemptCount(state model.State, invocationID string) int {
	count := 0
	for _, delivery := range state.InvocationDeliveries {
		if delivery.InvocationID == invocationID && !delivery.Manual {
			count++
		}
	}
	return count
}

func deliveryRetryPending(state model.State, invocationID string, now time.Time) bool {
	for _, delivery := range state.InvocationDeliveries {
		if delivery.InvocationID != invocationID {
			continue
		}
		if delivery.Status == "ATTEMPTED" && delivery.AttemptUntil != nil &&
			delivery.AttemptUntil.After(now) {
			return true
		}
		if delivery.NextRetryAt != nil && delivery.NextRetryAt.After(now) {
			return true
		}
	}
	return false
}

func effectiveRuntimeKind(runtime model.AgentRuntime) model.RuntimeKind {
	if runtime.Kind != "" {
		return runtime.Kind
	}
	if runtime.Connector == "INTERACTIVE" {
		return model.RuntimeKindInteractive
	}
	return model.RuntimeKindWorker
}

func effectiveConsumerMode(mode model.ConsumerMode) model.ConsumerMode {
	if mode == model.ConsumerModeInteractiveOnly ||
		mode == model.ConsumerModeWorkerOnly ||
		mode == model.ConsumerModeEither {
		return mode
	}
	return model.ConsumerModeEither
}

func consumerModeAllowsRuntime(mode model.ConsumerMode, kind model.RuntimeKind) bool {
	switch effectiveConsumerMode(mode) {
	case model.ConsumerModeInteractiveOnly:
		return kind == model.RuntimeKindInteractive
	case model.ConsumerModeWorkerOnly:
		return kind == model.RuntimeKindWorker
	default:
		return kind == model.RuntimeKindWorker || kind == model.RuntimeKindInteractive
	}
}

func deliveryBackoff(attempt int) time.Duration {
	backoff := baseDeliveryBackoff * time.Duration(1<<min(attempt-1, 8))
	return min(backoff, maxDeliveryBackoff)
}
