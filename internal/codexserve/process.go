package codexserve

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
	"sync"
	"sync/atomic"
)

const (
	defaultEventBuffer = 128
	maxStreamLineBytes = 2 * 1024 * 1024
)

// ProcessConfig describes one persistent Codex app-server process.
type ProcessConfig struct {
	Executable       string   `json:"executable"`
	WorkDir          string   `json:"work_dir"`
	Sandbox          string   `json:"sandbox"`
	AddDirs          []string `json:"add_dirs,omitempty"`
	IgnoreUserConfig bool     `json:"ignore_user_config,omitempty"`
	Model            string   `json:"model,omitempty"`
	ThreadID         string   `json:"thread_id,omitempty"`
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// Process owns one Codex app-server subprocess. Send is single-flight;
// subscribers are observational and can never inject input into the
// process.
type Process struct {
	config ProcessConfig

	turnMu   sync.Mutex
	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	dead     error
	nextID   int64
	pending  map[int64]chan rpcEnvelope
	threadID string

	subscribers map[chan []byte]struct{}
}

// Start validates config and starts a persistent Codex app-server process,
// then performs the initialize/initialized handshake and either resumes
// config.ThreadID (if set) or starts a fresh thread, recording whichever
// thread ID is actually in use so the caller can persist it.
func Start(ctx context.Context, config ProcessConfig) (*Process, error) {
	if err := validateProcessConfig(config); err != nil {
		return nil, err
	}
	process := &Process{config: config, pending: make(map[int64]chan rpcEnvelope), subscribers: make(map[chan []byte]struct{})}
	if err := process.start(ctx); err != nil {
		return nil, err
	}
	return process, nil
}

func validateProcessConfig(config ProcessConfig) error {
	if !filepath.IsAbs(config.Executable) || !filepath.IsAbs(config.WorkDir) {
		return errors.New("codexserve: executable and working directory must be absolute")
	}
	if config.Sandbox != "read-only" && config.Sandbox != "workspace-write" {
		return errors.New("codexserve: sandbox must be read-only or workspace-write")
	}
	return nil
}

// ThreadID returns the thread this process is currently bound to. Empty
// until the handshake in start() completes.
func (p *Process) ThreadID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.threadID
}

func (p *Process) start(ctx context.Context) error {
	arguments := []string{"app-server"}
	command := exec.CommandContext(context.WithoutCancel(ctx), p.config.Executable, arguments...)
	command.Dir = p.config.WorkDir
	command.Env = os.Environ()
	stdin, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("codexserve: open stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("codexserve: open stdout: %w", err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("codexserve: start Codex: %w", err)
	}

	p.mu.Lock()
	p.cmd = command
	p.stdin = stdin
	p.dead = nil
	p.mu.Unlock()
	go p.readLoop(command, stdout)

	if _, err := p.call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{"name": "agent-comms-codex-live", "title": "Agent Comms", "version": "0.0.1"},
	}); err != nil {
		return fmt.Errorf("codexserve: initialize: %w", err)
	}
	if err := p.notify("initialized", map[string]any{}); err != nil {
		return fmt.Errorf("codexserve: initialized notification: %w", err)
	}

	if p.config.ThreadID != "" {
		if _, err := p.call(ctx, "thread/resume", p.threadParams(p.config.ThreadID)); err == nil {
			p.mu.Lock()
			p.threadID = p.config.ThreadID
			p.mu.Unlock()
			return nil
		}
		// Configured or previously-cached thread ID that no longer
		// resolves (server restarted, history pruned) falls back to
		// starting a fresh thread below, rather than failing outright --
		// same principle opencode-live's session fallback already uses.
	}
	started, err := p.call(ctx, "thread/start", map[string]any{"cwd": p.config.WorkDir})
	if err != nil {
		return fmt.Errorf("codexserve: thread/start: %w", err)
	}
	var result struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(started.Result, &result); err != nil || result.Thread.ID == "" {
		return fmt.Errorf("codexserve: thread/start returned no thread id: %w", err)
	}
	p.mu.Lock()
	p.threadID = result.Thread.ID
	p.mu.Unlock()
	return nil
}

func (p *Process) threadParams(threadID string) map[string]any {
	return map[string]any{"threadId": threadID, "cwd": p.config.WorkDir}
}

func (p *Process) readLoop(command *exec.Cmd, stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxStreamLineBytes)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		p.broadcast(line)
		var envelope rpcEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			continue
		}
		if envelope.ID != nil {
			p.mu.Lock()
			channel, ok := p.pending[*envelope.ID]
			if ok {
				delete(p.pending, *envelope.ID)
			}
			p.mu.Unlock()
			if ok {
				channel <- envelope
			}
		}
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
	pending := p.pending
	p.pending = make(map[int64]chan rpcEnvelope)
	p.mu.Unlock()
	for _, channel := range pending {
		close(channel)
	}
}

func (p *Process) call(ctx context.Context, method string, params any) (rpcEnvelope, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return rpcEnvelope{}, err
	}
	id := atomic.AddInt64(&p.nextID, 1)
	channel := make(chan rpcEnvelope, 1)
	p.mu.Lock()
	if p.dead != nil {
		p.mu.Unlock()
		return rpcEnvelope{}, fmt.Errorf("codexserve: Codex process is not running: %w", p.dead)
	}
	p.pending[id] = channel
	stdin := p.stdin
	p.mu.Unlock()

	request := rpcEnvelope{JSONRPC: "2.0", ID: &id, Method: method, Params: raw}
	encoded, err := json.Marshal(request)
	if err != nil {
		return rpcEnvelope{}, err
	}
	if _, err := stdin.Write(append(encoded, '\n')); err != nil {
		wrapped := fmt.Errorf("codexserve: write request: %w", err)
		p.invalidate(wrapped)
		return rpcEnvelope{}, wrapped
	}
	select {
	case <-ctx.Done():
		return rpcEnvelope{}, ctx.Err()
	case response, ok := <-channel:
		if !ok {
			p.mu.Lock()
			dead := p.dead
			p.mu.Unlock()
			return rpcEnvelope{}, fmt.Errorf("codexserve: Codex exited before responding: %w", dead)
		}
		if len(response.Error) > 0 && string(response.Error) != "null" {
			return rpcEnvelope{}, fmt.Errorf("codexserve: %s failed: %s", method, string(response.Error))
		}
		return response, nil
	}
}

func (p *Process) notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	request := rpcEnvelope{JSONRPC: "2.0", Method: method, Params: raw}
	encoded, err := json.Marshal(request)
	if err != nil {
		return err
	}
	p.mu.Lock()
	stdin := p.stdin
	p.mu.Unlock()
	_, err = stdin.Write(append(encoded, '\n'))
	return err
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

func (p *Process) isDead() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dead != nil
}

// Send submits one user turn on this process's thread and returns the
// agent's aggregated final text. A process crash triggers one
// resume-and-retry attempt.
func (p *Process) Send(ctx context.Context, text string) (string, error) {
	p.turnMu.Lock()
	defer p.turnMu.Unlock()
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			if err := p.restart(ctx); err != nil {
				return "", fmt.Errorf("codexserve: restart after crash: %w", err)
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

func (p *Process) sendOnce(ctx context.Context, text string) (string, error) {
	p.mu.Lock()
	threadID := p.threadID
	p.mu.Unlock()
	if threadID == "" {
		return "", errors.New("codexserve: no active thread")
	}

	// Track the answer via notifications broadcast to a private
	// subscriber for the duration of this call, alongside the turn/start
	// request/response itself.
	events, cancel := p.Subscribe()
	defer cancel()

	if _, err := p.call(ctx, "turn/start", map[string]any{
		"threadId": threadID,
		"input":    []map[string]any{{"type": "text", "text": text}},
	}); err != nil {
		return "", fmt.Errorf("codexserve: turn/start: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case line, ok := <-events:
			if !ok {
				p.mu.Lock()
				dead := p.dead
				p.mu.Unlock()
				return "", fmt.Errorf("codexserve: Codex exited before completing turn: %w", dead)
			}
			var notification struct {
				Method string `json:"method"`
				Params struct {
					Item struct {
						Type  string `json:"type"`
						Phase string `json:"phase"`
						Text  string `json:"text"`
					} `json:"item"`
				} `json:"params"`
			}
			if err := json.Unmarshal(line, &notification); err != nil {
				continue
			}
			// The authoritative "this turn's answer is ready" signal,
			// confirmed live: an item/completed notification whose item
			// is an agentMessage in its final_answer phase. Do not key
			// off turn/completed alone -- confirmed live that it can be
			// absent or ambiguous across rapid sequential turns.
			if notification.Method == "item/completed" &&
				notification.Params.Item.Type == "agentMessage" &&
				notification.Params.Item.Phase == "final_answer" {
				return notification.Params.Item.Text, nil
			}
		}
	}
}

func (p *Process) restart(ctx context.Context) error {
	p.mu.Lock()
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	threadID := p.threadID
	p.mu.Unlock()
	p.config.ThreadID = threadID
	return p.start(ctx)
}

// Subscribe registers a bounded observer. Slow observers are disconnected
// instead of blocking Codex or other viewers.
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
