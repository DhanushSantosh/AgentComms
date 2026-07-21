package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/google/uuid"
)

const (
	defaultExecutionTimeout = 30 * time.Minute
	maxExecutionTimeout     = 2 * time.Hour
	heartbeatInterval       = 11 * time.Second
	heartbeatWriteInterval  = controlplane.MinHeartbeatInterval + time.Second
	maxAgentOutputBytes     = 1024 * 1024
	maxResultMessageBytes   = 1200
	maxCompletionBytes      = 240
	maxFailureReasonBytes   = 600
	maxClaudeBudgetUSD      = 100
	actionStartMarker       = "<agent-comms-invoke>"
	actionEndMarker         = "</agent-comms-invoke>"
	actionLinePrefix        = "AGENT_COMMS_INVOKE:"
)

type invocationAction struct {
	Target           string   `json:"target"`
	Instruction      string   `json:"instruction"`
	ExpectedResult   string   `json:"expected_result"`
	Priority         string   `json:"priority"`
	Scopes           []string `json:"scopes,omitempty"`
	ExpiresInSeconds int      `json:"expires_in_seconds"`
}

type Config struct {
	Service               *service.Service
	Actor                 string
	RuntimeID             string
	SessionID             string
	Adapter               string
	Executable            string
	WorkDir               string
	Model                 string
	PermissionMode        string
	Sandbox               string
	CodexAddDirs          []string
	CodexIgnoreUserConfig bool
	ExecutionTimeout      time.Duration
	ListenWait            time.Duration
	ClaudeBudgetUSD       float64
	AgentCommsPath        string
	Once                  bool
	Status                func(string)
}

type Worker struct {
	config        Config
	adapter       Adapter
	run           func(context.Context, model.Invocation) (string, error)
	heartbeatMu   sync.Mutex
	lastHeartbeat time.Time
}

func New(config Config) (*Worker, error) {
	if err := validateConfig(&config); err != nil {
		return nil, err
	}
	adapter, err := resolveAdapter(config.Adapter)
	if err != nil {
		return nil, err
	}
	if err := adapter.Validate(&config); err != nil {
		return nil, err
	}
	worker := &Worker{config: config, adapter: adapter}
	worker.run = worker.runAgent
	return worker, nil
}

func validateConfig(config *Config) error {
	if config.Service == nil {
		return errors.New("worker service is required")
	}
	if strings.TrimSpace(config.Actor) == "" || strings.TrimSpace(config.RuntimeID) == "" {
		return errors.New("worker actor and runtime ID are required")
	}
	config.SessionID = strings.TrimSpace(config.SessionID)
	if config.SessionID != "" {
		if _, err := uuid.Parse(config.SessionID); err != nil {
			return errors.New("worker session ID must be a valid UUID")
		}
	}
	config.Adapter = strings.ToLower(strings.TrimSpace(config.Adapter))
	if !filepath.IsAbs(config.Executable) {
		return errors.New("worker executable must be an absolute path")
	}
	info, err := os.Stat(config.Executable)
	if err != nil {
		return fmt.Errorf("inspect worker executable: %w", err)
	}
	if info.IsDir() || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
		return errors.New("worker executable is not executable")
	}
	if !filepath.IsAbs(config.WorkDir) {
		return errors.New("worker working directory must be absolute")
	}
	workDirInfo, err := os.Stat(config.WorkDir)
	if err != nil {
		return fmt.Errorf("inspect worker working directory: %w", err)
	}
	if !workDirInfo.IsDir() {
		return errors.New("worker working directory must be a directory")
	}
	if config.ExecutionTimeout == 0 {
		config.ExecutionTimeout = defaultExecutionTimeout
	}
	if config.ExecutionTimeout < time.Second || config.ExecutionTimeout > maxExecutionTimeout {
		return fmt.Errorf("worker execution timeout must be from 1s to %s", maxExecutionTimeout)
	}
	if config.ListenWait == 0 {
		config.ListenWait = controlplane.MaxInvocationListen
	}
	if config.ListenWait < time.Second || config.ListenWait > controlplane.MaxInvocationListen {
		return fmt.Errorf("worker listen wait must be from 1s to %s", controlplane.MaxInvocationListen)
	}
	if config.Status == nil {
		config.Status = func(string) {}
	}
	return nil
}

func (w *Worker) Run(ctx context.Context) error {
	state, err := w.config.Service.State()
	if err != nil {
		return err
	}
	runtime, exists := state.AgentRuntimes[w.config.RuntimeID]
	if !exists || runtime.AgentID != w.config.Actor {
		return errors.New("worker runtime must be registered to the active actor")
	}
	if runtime.Status == "DRAINING" || runtime.Status == "REVOKED" {
		return fmt.Errorf("worker runtime is %s", strings.ToLower(runtime.Status))
	}
	if !runtime.LastSeenAt.IsZero() && time.Since(runtime.LastSeenAt) < controlplane.MinHeartbeatInterval {
		w.lastHeartbeat = runtime.LastSeenAt
	}
	if err = w.heartbeat(nil); err != nil {
		return err
	}
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		w.config.Status("listening for invocations")
		invocation, found, listenErr := w.config.Service.ListenInvocation(
			w.config.Actor,
			w.config.RuntimeID,
			w.config.ListenWait,
		)
		if listenErr != nil {
			return listenErr
		}
		if !found {
			if heartbeatErr := w.heartbeat(nil); heartbeatErr != nil {
				return heartbeatErr
			}
			if w.config.Once {
				return nil
			}
			continue
		}
		if processErr := w.process(ctx, invocation); processErr != nil {
			w.config.Status("invocation " + invocation.ID + " requires attention: " + processErr.Error())
			if w.config.Once {
				return processErr
			}
		}
		if w.config.Once {
			return nil
		}
	}
}

func (w *Worker) process(ctx context.Context, invocation model.Invocation) error {
	w.config.Status("claiming invocation " + invocation.ID)
	if _, err := w.config.Service.Execute(w.config.Actor, "invocation.claim", invocation.ID,
		model.InvocationClaimed{RuntimeID: w.config.RuntimeID}); err != nil {
		return err
	}
	if _, err := w.config.Service.Execute(w.config.Actor, "invocation.start", invocation.ID,
		model.InvocationProgress{Summary: "Autonomous " + w.config.Adapter + " worker started"}); err != nil {
		return err
	}
	if err := w.heartbeat([]string{invocation.ID}); err != nil {
		return w.wait(invocation.ID, "runtime heartbeat failed: "+err.Error())
	}
	executionCtx, cancel := context.WithTimeout(ctx, w.config.ExecutionTimeout)
	defer cancel()
	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go w.heartbeatLoop(stopHeartbeat, heartbeatDone, invocation.ID)
	w.config.Status("executing invocation " + invocation.ID + " with " + w.config.Adapter)
	output, runErr := w.run(executionCtx, invocation)
	close(stopHeartbeat)
	<-heartbeatDone
	if runErr != nil {
		return w.wait(invocation.ID, "agent execution failed: "+runErr.Error())
	}
	output = strings.TrimSpace(output)
	if output == "" {
		output = "Agent completed without a textual result."
	}
	output, followUpID, actionErr := w.executeInvocationAction(output)
	if actionErr != nil {
		return w.wait(invocation.ID, "agent follow-up action failed: "+actionErr.Error())
	}
	if followUpID != "" {
		output = strings.TrimSpace(output) + "\n\nCreated follow-up invocation: " + followUpID
	}
	messageID := "result-" + invocation.ID + "-" + uuid.NewString()
	message := model.MessagePosted{
		Kind: "FYI", To: []string{invocation.RequestedBy},
		Subject: "Invocation result: " + invocation.ID,
		Body:    truncateUTF8(output, maxResultMessageBytes), TaskID: invocation.TaskID,
	}
	if _, err := w.config.Service.Execute(w.config.Actor, "message.post", messageID, message); err != nil {
		return w.wait(invocation.ID, "result publication failed: "+err.Error())
	}
	summary := truncateUTF8(singleLine(output), maxCompletionBytes)
	if _, err := w.config.Service.Execute(w.config.Actor, "invocation.complete", invocation.ID,
		model.InvocationCompleted{Summary: summary, ResultMessageID: messageID}); err != nil {
		return w.wait(invocation.ID, "completion publication failed: "+err.Error())
	}
	if err := w.heartbeat(nil); err != nil {
		return err
	}
	w.config.Status("completed invocation " + invocation.ID)
	return nil
}

func (w *Worker) executeInvocationAction(output string) (string, string, error) {
	cleaned, action, err := parseInvocationAction(output)
	if err != nil || action == nil {
		return cleaned, "", err
	}
	if action.Priority == "" {
		action.Priority = "NORMAL"
	}
	if action.ExpiresInSeconds == 0 {
		action.ExpiresInSeconds = 600
	}
	if action.ExpiresInSeconds < 60 || action.ExpiresInSeconds > 86400 {
		return cleaned, "", errors.New("follow-up expiry must be from 60 to 86400 seconds")
	}
	invocationID := "inv-" + uuid.NewString()
	deadline := time.Now().UTC().Add(time.Duration(action.ExpiresInSeconds) * time.Second)
	_, err = w.config.Service.Execute(w.config.Actor, "invocation.request", invocationID,
		model.InvocationRequested{
			Target: action.Target, Instruction: action.Instruction,
			ExpectedResult: action.ExpectedResult, Scopes: action.Scopes,
			Priority: action.Priority,
			Deadline: &deadline,
		})
	if err != nil {
		return cleaned, "", err
	}
	return cleaned, invocationID, nil
}

func parseInvocationAction(output string) (string, *invocationAction, error) {
	if prefixStart := strings.Index(output, actionLinePrefix); prefixStart >= 0 {
		valueStart := prefixStart + len(actionLinePrefix)
		valueEnd := strings.IndexByte(output[valueStart:], '\n')
		if valueEnd < 0 {
			valueEnd = len(output)
		} else {
			valueEnd += valueStart
		}
		if strings.Contains(output[valueEnd:], actionLinePrefix) {
			return output, nil, errors.New("only one follow-up invocation is allowed per result")
		}
		action, err := decodeInvocationAction(strings.TrimSpace(output[valueStart:valueEnd]))
		if err != nil {
			return output, nil, err
		}
		cleaned := strings.TrimSpace(output[:prefixStart] + output[valueEnd:])
		return cleaned, action, nil
	}
	start := strings.Index(output, actionStartMarker)
	if start < 0 {
		return output, nil, nil
	}
	endOffset := strings.Index(output[start+len(actionStartMarker):], actionEndMarker)
	if endOffset < 0 {
		return output, nil, errors.New("follow-up action is missing its closing marker")
	}
	end := start + len(actionStartMarker) + endOffset
	if strings.Contains(output[end+len(actionEndMarker):], actionStartMarker) {
		return output, nil, errors.New("only one follow-up invocation is allowed per result")
	}
	raw := strings.TrimSpace(output[start+len(actionStartMarker) : end])
	action, err := decodeInvocationAction(raw)
	if err != nil {
		return output, nil, err
	}
	cleaned := strings.TrimSpace(output[:start] + output[end+len(actionEndMarker):])
	return cleaned, action, nil
}

func decodeInvocationAction(raw string) (*invocationAction, error) {
	var action invocationAction
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&action); err != nil {
		return nil, fmt.Errorf("decode follow-up action: %w", err)
	}
	if strings.TrimSpace(action.Target) == "" || strings.TrimSpace(action.Instruction) == "" {
		return nil, errors.New("follow-up target and instruction are required")
	}
	return &action, nil
}

func (w *Worker) wait(invocationID, reason string) error {
	boundedReason := truncateUTF8(reason, maxFailureReasonBytes)
	_, err := w.config.Service.Execute(w.config.Actor, "invocation.wait", invocationID,
		model.InvocationWaiting{Reason: boundedReason})
	if err != nil {
		return fmt.Errorf("%s; recording wait state: %w", boundedReason, err)
	}
	_ = w.heartbeat([]string{invocationID})
	return errors.New(boundedReason)
}

func (w *Worker) heartbeat(active []string) error {
	w.heartbeatMu.Lock()
	defer w.heartbeatMu.Unlock()
	now := time.Now()
	if !w.lastHeartbeat.IsZero() && now.Sub(w.lastHeartbeat) < heartbeatWriteInterval {
		return nil
	}
	_, err := w.config.Service.Execute(w.config.Actor, "runtime.heartbeat", w.config.RuntimeID,
		model.RuntimeHeartbeat{Health: "HEALTHY", ActiveInvocations: active})
	if err == nil {
		w.lastHeartbeat = now
	}
	return err
}

func (w *Worker) heartbeatLoop(stop <-chan struct{}, done chan<- struct{}, invocationID string) {
	defer close(done)
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if err := w.heartbeat([]string{invocationID}); err != nil {
				w.config.Status("heartbeat warning: " + err.Error())
			}
		}
	}
}

func (w *Worker) runAgent(ctx context.Context, invocation model.Invocation) (string, error) {
	prompt := w.adapter.Prompt(w.config.Actor, invocation)
	arguments := w.arguments()
	command := exec.CommandContext(ctx, w.config.Executable, arguments...)
	command.Dir = w.config.WorkDir
	command.Env = os.Environ()
	command.Stdin = strings.NewReader(prompt)
	stdout := &boundedBuffer{limit: maxAgentOutputBytes}
	stderr := &boundedBuffer{limit: maxAgentOutputBytes}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("execution deadline reached: %w", ctx.Err())
	}
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", errors.New(truncateUTF8(detail, maxFailureReasonBytes))
	}
	if stdout.truncated {
		return "", fmt.Errorf("agent output exceeded %d bytes", maxAgentOutputBytes)
	}
	return stdout.String(), nil
}

func (w *Worker) arguments() []string {
	return w.adapter.Arguments(w.config)
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	bounded := value[:limit]
	for !utf8.ValidString(bounded) {
		bounded = bounded[:len(bounded)-1]
	}
	return strings.TrimSpace(bounded)
}

type boundedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	originalLength := len(value)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return originalLength, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(value)
	return originalLength, nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

var _ io.Writer = (*boundedBuffer)(nil)
