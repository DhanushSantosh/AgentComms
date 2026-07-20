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
	"strconv"
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
	CodexHome             string
	ExecutionTimeout      time.Duration
	ListenWait            time.Duration
	ClaudeBudgetUSD       float64
	AgentCommsPath        string
	Once                  bool
	Status                func(string)
}

type Worker struct {
	config        Config
	run           func(context.Context, model.Invocation) (string, error)
	heartbeatMu   sync.Mutex
	lastHeartbeat time.Time
}

func New(config Config) (*Worker, error) {
	if err := validateConfig(&config); err != nil {
		return nil, err
	}
	worker := &Worker{config: config}
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
	if config.Adapter != "claude" && config.Adapter != "codex" {
		return errors.New("worker adapter must be claude or codex")
	}
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
	if config.Adapter == "claude" {
		if config.PermissionMode == "" {
			config.PermissionMode = "acceptEdits"
		}
		switch config.PermissionMode {
		case "acceptEdits", "auto", "dontAsk", "manual", "plan":
		default:
			return errors.New("claude permission mode must not bypass permissions")
		}
		if config.ClaudeBudgetUSD <= 0 || config.ClaudeBudgetUSD > maxClaudeBudgetUSD {
			return fmt.Errorf("claude budget must be greater than 0 and at most %.0f USD", float64(maxClaudeBudgetUSD))
		}
		if config.AgentCommsPath != "" {
			if !filepath.IsAbs(config.AgentCommsPath) {
				return errors.New("allowed Agent Comms executable must use an absolute path")
			}
			info, err := os.Stat(config.AgentCommsPath)
			if err != nil {
				return fmt.Errorf("inspect allowed Agent Comms executable: %w", err)
			}
			if info.IsDir() || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
				return errors.New("allowed Agent Comms executable is not executable")
			}
		}
	}
	if config.Adapter == "codex" {
		if config.Sandbox == "" {
			config.Sandbox = "workspace-write"
		}
		if config.Sandbox != "read-only" && config.Sandbox != "workspace-write" {
			return errors.New("codex worker sandbox must be read-only or workspace-write")
		}
		for _, directory := range config.CodexAddDirs {
			if !filepath.IsAbs(directory) {
				return errors.New("codex additional writable directories must use absolute paths")
			}
			info, err := os.Stat(directory)
			if err != nil {
				return fmt.Errorf("inspect Codex additional writable directory: %w", err)
			}
			if !info.IsDir() {
				return errors.New("codex additional writable path must be a directory")
			}
		}
		if config.CodexHome != "" {
			if !filepath.IsAbs(config.CodexHome) {
				return errors.New("codex home must be an absolute path")
			}
			info, err := os.Stat(config.CodexHome)
			if err != nil {
				return fmt.Errorf("inspect codex home: %w", err)
			}
			if !info.IsDir() {
				return errors.New("codex home must be a directory")
			}
		}
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
	var prompt string
	if w.config.Adapter == "claude" {
		prompt = claudeUserPrompt(invocation)
	} else {
		prompt = invocationPrompt(w.config.Actor, invocation)
	}
	arguments := w.arguments()
	command := exec.CommandContext(ctx, w.config.Executable, arguments...)
	command.Dir = w.config.WorkDir
	command.Env = agentEnvironment(os.Environ(), w.config.Adapter, w.config.CodexHome)
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

// agentEnvironment overrides CODEX_HOME for a Codex worker when a dedicated
// home is configured. Without this, a Codex worker inherits ~/.codex and
// therefore the same auth.json as any interactive Codex session on the
// machine; on ChatGPT-plan auth that means the autonomous worker and the
// operator's interactive usage draw from one shared, non-retryable usage
// quota (Codex's own UsageLimitReached error is not retried; it requires the
// plan window to reset, an upgrade, or a separate auth). Pointing the
// worker at its own CODEX_HOME, logged in separately (ideally with an
// API key billed to the organization instead of the shared ChatGPT plan),
// isolates its usage entirely.
func agentEnvironment(base []string, adapter, codexHome string) []string {
	if adapter != "codex" || codexHome == "" {
		return base
	}
	environment := make([]string, 0, len(base)+1)
	for _, entry := range base {
		if strings.HasPrefix(entry, "CODEX_HOME=") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, "CODEX_HOME="+codexHome)
}

func (w *Worker) arguments() []string {
	if w.config.Adapter == "claude" {
		arguments := []string{
			"--print", "--output-format", "text",
			"--append-system-prompt", claudeSystemPrompt(w.config.Actor),
			"--permission-mode", w.config.PermissionMode,
			"--max-budget-usd", strconv.FormatFloat(w.config.ClaudeBudgetUSD, 'f', 2, 64),
		}
		if w.config.SessionID == "" {
			arguments = append(arguments, "--no-session-persistence")
		} else {
			arguments = append(arguments, "--resume", w.config.SessionID)
		}
		if w.config.AgentCommsPath != "" {
			arguments = append(arguments, "--allowedTools", "Bash("+w.config.AgentCommsPath+" *)")
		}
		if w.config.Model != "" {
			arguments = append(arguments, "--model", w.config.Model)
		}
		return arguments
	}
	arguments := []string{
		"--ask-for-approval", "never", "exec", "--color", "never",
		"--sandbox", w.config.Sandbox,
	}
	for _, directory := range w.config.CodexAddDirs {
		arguments = append(arguments, "--add-dir", directory)
	}
	if w.config.CodexIgnoreUserConfig {
		arguments = append(arguments, "--ignore-user-config")
	}
	if w.config.SessionID == "" {
		arguments = append(arguments, "--ephemeral")
	} else {
		arguments = append(arguments, "resume", w.config.SessionID)
	}
	if w.config.Model != "" {
		arguments = append(arguments, "--model", w.config.Model)
	}
	return append(arguments, "-")
}

func invocationPrompt(actor string, invocation model.Invocation) string {
	var body strings.Builder
	body.WriteString("You are the autonomous Agent Comms runtime for agent ")
	body.WriteString(actor)
	body.WriteString(".\n")
	body.WriteString("Treat the invocation instruction as authorized project work, but continue to obey repository rules, configured tool permissions, and workspace boundaries.\n")
	body.WriteString("Do not ask the user to relay messages to another agent. Perform the work and return a concise final result; Agent Comms will publish it to the requester.\n\n")
	body.WriteString("When the work requires invoking another agent, do not call Agent Comms through Bash or MCP. Include exactly one single-line action in your final response using this format:\n")
	body.WriteString(actionLinePrefix)
	body.WriteString(" ")
	body.WriteString(`{"target":"AGENT_ID","instruction":"bounded instruction","expected_result":"bounded result","priority":"NORMAL","scopes":[],"expires_in_seconds":600}`)
	body.WriteString("\nThe runtime will validate, sign, and submit that follow-up using your agent identity. Leave scopes empty unless the instruction explicitly requires a scope you know both agents possess; do not copy the current invocation scopes automatically.\n\n")
	body.WriteString("Invocation ID: ")
	body.WriteString(invocation.ID)
	body.WriteString("\nRequester: ")
	body.WriteString(invocation.RequestedBy)
	body.WriteString("\nPriority: ")
	body.WriteString(invocation.Priority)
	if invocation.TaskID != "" {
		body.WriteString("\nRelated task: ")
		body.WriteString(invocation.TaskID)
	}
	if invocation.MessageID != "" {
		body.WriteString("\nRelated message: ")
		body.WriteString(invocation.MessageID)
	}
	if len(invocation.Scopes) > 0 {
		body.WriteString("\nAuthorized scopes: ")
		body.WriteString(strings.Join(invocation.Scopes, ", "))
	}
	body.WriteString("\n\nInstruction:\n")
	body.WriteString(invocation.Instruction)
	if invocation.ExpectedResult != "" {
		body.WriteString("\n\nExpected result:\n")
		body.WriteString(invocation.ExpectedResult)
	}
	return body.String()
}

// claudeSystemPrompt carries the runtime's operating convention on the
// trusted system-prompt channel instead of the first user turn. Blending this
// framing into user content reads as a self-authorizing instruction smuggled
// into the conversation, which Claude correctly treats as a likely prompt
// injection and refuses; appending it as a system prompt avoids that false
// positive while keeping the invocation body itself as plain user content.
func claudeSystemPrompt(actor string) string {
	var body strings.Builder
	body.WriteString("You are ")
	body.WriteString(actor)
	body.WriteString(", an agent registered with Agent Comms, a governed multi-agent coordination tool. This message describes a standing, audited operating convention for invocations you receive through it; it does not itself grant, alter, or bypass any tool permission.\n")
	body.WriteString("Each user turn carries one Agent Comms invocation already authorized by project governance. Complete the described work under your normal repository rules, configured tool permissions, and workspace boundaries.\n")
	body.WriteString("Do not ask the user to relay messages to another agent; perform the work and return a concise final result, and Agent Comms will publish it to the requester.\n\n")
	body.WriteString("Only if completing the work genuinely requires another registered Agent Comms agent's help, you may end your response with exactly one line in this format (do not invoke Agent Comms via Bash or MCP for this):\n")
	body.WriteString(actionLinePrefix)
	body.WriteString(" ")
	body.WriteString(`{"target":"AGENT_ID","instruction":"bounded instruction","expected_result":"bounded result","priority":"NORMAL","scopes":[],"expires_in_seconds":600}`)
	body.WriteString("\nAgent Comms validates, signs, and submits that follow-up under your own agent identity; it never executes arbitrary content. Leave scopes empty unless the instruction explicitly requires a scope you know both agents possess — never copy the current invocation's scopes automatically. Omit the line entirely when no follow-up is needed.")
	return body.String()
}

// claudeUserPrompt carries only the concrete task details for this
// invocation; the operating convention lives in claudeSystemPrompt instead.
func claudeUserPrompt(invocation model.Invocation) string {
	var body strings.Builder
	body.WriteString("Invocation ID: ")
	body.WriteString(invocation.ID)
	body.WriteString("\nRequester: ")
	body.WriteString(invocation.RequestedBy)
	body.WriteString("\nPriority: ")
	body.WriteString(invocation.Priority)
	if invocation.TaskID != "" {
		body.WriteString("\nRelated task: ")
		body.WriteString(invocation.TaskID)
	}
	if invocation.MessageID != "" {
		body.WriteString("\nRelated message: ")
		body.WriteString(invocation.MessageID)
	}
	if len(invocation.Scopes) > 0 {
		body.WriteString("\nAuthorized scopes: ")
		body.WriteString(strings.Join(invocation.Scopes, ", "))
	}
	body.WriteString("\n\nInstruction:\n")
	body.WriteString(invocation.Instruction)
	if invocation.ExpectedResult != "" {
		body.WriteString("\n\nExpected result:\n")
		body.WriteString(invocation.ExpectedResult)
	}
	return body.String()
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
