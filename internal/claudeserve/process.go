package claudeserve

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultEventBuffer = 128
	maxStreamLineBytes = 2 * 1024 * 1024
)

// ProcessConfig describes one persistent Claude Code stream-json process.
type ProcessConfig struct {
	Executable     string  `json:"executable"`
	WorkDir        string  `json:"work_dir"`
	PermissionMode string  `json:"permission_mode"`
	SystemPrompt   string  `json:"system_prompt"`
	AgentCommsPath string  `json:"agent_comms_path,omitempty"`
	Model          string  `json:"model,omitempty"`
	SessionID      string  `json:"session_id,omitempty"`
	MaxBudgetUSD   float64 `json:"max_budget_usd"`
}

type streamResult struct {
	Type              string          `json:"type"`
	Subtype           string          `json:"subtype"`
	IsError           bool            `json:"is_error"`
	Result            string          `json:"result"`
	PermissionDenials json.RawMessage `json:"permission_denials"`
}

type streamMessage struct {
	Type    string `json:"type"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// Process owns one Claude subprocess. Send is single-flight; subscribers are
// observational and can never inject input into the process.
type Process struct {
	config ProcessConfig

	turnMu      sync.Mutex
	mu          sync.Mutex
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	lines       chan []byte
	dead        error
	subscribers map[chan []byte]struct{}
}

// Start validates config and starts a persistent Claude stream-json process.
func Start(ctx context.Context, config ProcessConfig) (*Process, error) {
	if err := validateProcessConfig(config); err != nil {
		return nil, err
	}
	process := &Process{config: config, subscribers: make(map[chan []byte]struct{})}
	if err := process.start(ctx); err != nil {
		return nil, err
	}
	return process, nil
}

func validateProcessConfig(config ProcessConfig) error {
	if !filepath.IsAbs(config.Executable) || !filepath.IsAbs(config.WorkDir) {
		return errors.New("claudeserve: executable and working directory must be absolute")
	}
	if strings.TrimSpace(config.PermissionMode) == "" {
		return errors.New("claudeserve: permission mode is required")
	}
	if config.MaxBudgetUSD <= 0 {
		return errors.New("claudeserve: maximum budget must be greater than zero")
	}
	return nil
}

func (p *Process) start(ctx context.Context) error {
	arguments := []string{
		"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--replay-user-messages",
		"--append-system-prompt", p.config.SystemPrompt,
		"--permission-mode", p.config.PermissionMode,
		"--max-budget-usd", strconv.FormatFloat(p.config.MaxBudgetUSD, 'f', 2, 64),
	}
	if p.config.SessionID != "" {
		if SessionExists(p.config.WorkDir, p.config.SessionID) {
			arguments = append(arguments, "--resume", p.config.SessionID)
		} else {
			arguments = append(arguments, "--session-id", p.config.SessionID)
		}
	}
	if p.config.AgentCommsPath != "" {
		arguments = append(arguments, "--allowedTools", "Bash("+p.config.AgentCommsPath+" *)")
	}
	if p.config.Model != "" {
		arguments = append(arguments, "--model", p.config.Model)
	}

	command := exec.CommandContext(context.WithoutCancel(ctx), p.config.Executable, arguments...)
	command.Dir = p.config.WorkDir
	command.Env = os.Environ()
	stdin, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("claudeserve: open stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("claudeserve: open stdout: %w", err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("claudeserve: start Claude: %w", err)
	}

	p.mu.Lock()
	p.cmd = command
	p.stdin = stdin
	p.lines = make(chan []byte, defaultEventBuffer)
	p.dead = nil
	lines := p.lines
	p.mu.Unlock()
	go p.readLoop(command, stdout, lines)
	return nil
}

func (p *Process) readLoop(command *exec.Cmd, stdout io.Reader, lines chan []byte) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxStreamLineBytes)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		p.broadcast(line)
		lines <- line
	}
	err := scanner.Err()
	waitErr := command.Wait()
	if err == nil {
		err = waitErr
	}
	if err == nil {
		err = io.EOF
	}
	p.mu.Lock()
	if p.cmd == command {
		p.dead = err
	}
	p.mu.Unlock()
	close(lines)
}

// Send submits one user turn and returns its aggregated textual result. A
// process crash triggers one resume-and-retry attempt.
func (p *Process) Send(ctx context.Context, text string) (string, error) {
	p.turnMu.Lock()
	defer p.turnMu.Unlock()
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			if err := p.restart(ctx); err != nil {
				return "", fmt.Errorf("claudeserve: restart after crash: %w", err)
			}
		}
		output, err := p.sendOnce(ctx, text)
		if err == nil {
			return output, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			p.invalidate(ctx.Err())
			break
		}
		if !p.isDead() {
			break
		}
	}
	return "", lastErr
}

func (p *Process) invalidate(reason error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dead = reason
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

func (p *Process) sendOnce(ctx context.Context, text string) (string, error) {
	p.mu.Lock()
	stdin, lines, dead := p.stdin, p.lines, p.dead
	p.mu.Unlock()
	if dead != nil || stdin == nil || lines == nil {
		return "", fmt.Errorf("claudeserve: Claude process is not running: %w", dead)
	}
	envelope := map[string]any{"type": "user", "message": map[string]any{
		"role": "user", "content": []map[string]string{{"type": "text", "text": text}},
	}}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	if _, err := stdin.Write(append(raw, '\n')); err != nil {
		wrapped := fmt.Errorf("claudeserve: write prompt: %w", err)
		p.invalidate(wrapped)
		return "", wrapped
	}

	var output strings.Builder
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case line, ok := <-lines:
			if !ok {
				p.mu.Lock()
				dead = p.dead
				p.mu.Unlock()
				return "", fmt.Errorf("claudeserve: Claude exited before completing turn: %w", dead)
			}
			var result streamResult
			if err := json.Unmarshal(line, &result); err == nil && result.Type == "result" {
				if result.IsError || result.Subtype != "success" {
					return "", fmt.Errorf("claudeserve: Claude turn failed (%s): %s", result.Subtype, strings.TrimSpace(result.Result))
				}
				if hasPermissionDenials(result.PermissionDenials) {
					return "", errors.New("claudeserve: Claude denied one or more permissions")
				}
				if strings.TrimSpace(result.Result) != "" {
					return result.Result, nil
				}
				return output.String(), nil
			}
			var message streamMessage
			if err := json.Unmarshal(line, &message); err == nil && message.Type == "assistant" {
				for _, block := range message.Message.Content {
					if block.Type == "text" {
						output.WriteString(block.Text)
					}
				}
			}
		}
	}
}

func hasPermissionDenials(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null" && trimmed != "[]"
}

func (p *Process) restart(ctx context.Context) error {
	p.mu.Lock()
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	p.mu.Unlock()
	return p.start(ctx)
}

func (p *Process) isDead() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dead != nil
}

// Subscribe registers a bounded observer. Slow observers are disconnected
// instead of blocking Claude or other viewers.
func (p *Process) Subscribe() (<-chan []byte, func()) {
	channel := make(chan []byte, defaultEventBuffer)
	p.mu.Lock()
	p.subscribers[channel] = struct{}{}
	p.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			p.mu.Lock()
			if _, ok := p.subscribers[channel]; ok {
				delete(p.subscribers, channel)
				close(channel)
			}
			p.mu.Unlock()
		})
	}
	return channel, cancel
}

func (p *Process) broadcast(line []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for subscriber := range p.subscribers {
		select {
		case subscriber <- append([]byte(nil), line...):
		default:
			delete(p.subscribers, subscriber)
			close(subscriber)
		}
	}
}

// Close terminates the owned subprocess.
func (p *Process) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}

// SessionExists reports whether Claude Code has a persisted session for the
// working directory and ID.
func SessionExists(workDir, sessionID string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		abs = workDir
	}
	slug := strings.ReplaceAll(abs, "/", "-")
	_, err = os.Stat(filepath.Join(home, ".claude", "projects", slug, sessionID+".jsonl"))
	return err == nil
}
