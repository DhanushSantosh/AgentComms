package codexserve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultServeAddress is fixed rather than OS-assigned so a running
	// broker can always be found by address alone, even with no cache
	// file at all -- the same lesson opencode-live's and claude-live's
	// own fixed-port fixes already carry: a lost or reset cache must not
	// silently orphan a still-running broker behind a duplicate spawn on
	// a different port. Chosen to avoid colliding with opencode-live's
	// 4096 and claude-live's 4097.
	DefaultServeAddress  = "127.0.0.1:4098"
	serverStartupTimeout = 30 * time.Second
	maxRequestBytes      = 1024 * 1024
)

var runtimeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type ServerInfo struct {
	BaseURL string `json:"base_url"`
}

func ServerInfoPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".agent-comms", "cache", "codex-serve.json")
}

func DefaultServeBaseURL() string { return "http://" + DefaultServeAddress }

type Broker struct {
	mu        sync.RWMutex
	processes map[string]*Process
}

func NewBroker() *Broker { return &Broker{processes: make(map[string]*Process)} }

func (b *Broker) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte("ok\n"))
	})
	mux.HandleFunc("POST /runtimes/{runtimeID}/register", b.register)
	mux.HandleFunc("POST /runtimes/{runtimeID}/prompt", b.prompt)
	mux.HandleFunc("GET /runtimes/{runtimeID}/events", b.events)
	return mux
}

func (b *Broker) register(response http.ResponseWriter, request *http.Request) {
	runtimeID, ok := checkedRuntimeID(response, request)
	if !ok {
		return
	}
	var config ProcessConfig
	if !decodeRequest(response, request, &config) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing, exists := b.processes[runtimeID]; exists {
		existingConfig := existing.config
		existingConfig.ThreadID = ""
		requestConfig := config
		requestConfig.ThreadID = ""
		if !reflect.DeepEqual(existingConfig, requestConfig) {
			http.Error(response, "runtime is already registered with different process configuration", http.StatusConflict)
			return
		}
		writeJSON(response, http.StatusOK, map[string]string{"thread_id": existing.ThreadID()})
		return
	}
	process, err := Start(request.Context(), config)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	b.processes[runtimeID] = process
	writeJSON(response, http.StatusCreated, map[string]string{"thread_id": process.ThreadID()})
}

func (b *Broker) prompt(response http.ResponseWriter, request *http.Request) {
	runtimeID, ok := checkedRuntimeID(response, request)
	if !ok {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if !decodeRequest(response, request, &body) {
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		http.Error(response, "prompt text is required", http.StatusBadRequest)
		return
	}
	process := b.process(runtimeID)
	if process == nil {
		http.Error(response, "runtime is not registered", http.StatusNotFound)
		return
	}
	output, err := process.Send(request.Context(), body.Text)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"output": output})
}

func (b *Broker) events(response http.ResponseWriter, request *http.Request) {
	runtimeID, ok := checkedRuntimeID(response, request)
	if !ok {
		return
	}
	process := b.process(runtimeID)
	if process == nil {
		http.Error(response, "runtime is not registered", http.StatusNotFound)
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		http.Error(response, "streaming is unavailable", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	events, cancel := process.Subscribe()
	defer cancel()
	_, _ = response.Write([]byte(": connected\n\n"))
	flusher.Flush()
	for {
		select {
		case <-request.Context().Done():
			return
		case event, open := <-events:
			if !open {
				return
			}
			_, _ = fmt.Fprintf(response, "data: %s\n\n", event)
			flusher.Flush()
		}
	}
}

func (b *Broker) process(runtimeID string) *Process {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.processes[runtimeID]
}

func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for runtimeID, process := range b.processes {
		_ = process.Close()
		delete(b.processes, runtimeID)
	}
}

func checkedRuntimeID(response http.ResponseWriter, request *http.Request) (string, bool) {
	runtimeID := request.PathValue("runtimeID")
	if !runtimeIDPattern.MatchString(runtimeID) {
		http.Error(response, "invalid runtime ID", http.StatusBadRequest)
		return "", false
	}
	return runtimeID, true
}

func decodeRequest(response http.ResponseWriter, request *http.Request, out any) bool {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		http.Error(response, "invalid JSON request: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

// Serve runs a foreground broker on a loopback address until ctx is canceled.
func Serve(ctx context.Context, address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("codexserve: invalid listen address: %w", err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return errors.New("codexserve: broker must listen on a loopback address")
	}
	broker := NewBroker()
	defer broker.Close()
	server := &http.Server{
		Addr: address, Handler: broker.Handler(),
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 2 * time.Minute,
	}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// EnsureServer reuses a healthy cached broker, probes the well-known port
// when the cache is absent or stale, and only then spawns a detached broker.
func EnsureServer(ctx context.Context, projectRoot, workDir string) (string, error) {
	path := ServerInfoPath(projectRoot)
	if baseURL, ok := resolveRunningServer(ctx, path, DefaultServeBaseURL()); ok {
		if err := saveServerInfo(path, ServerInfo{BaseURL: baseURL}); err != nil {
			return "", err
		}
		return baseURL, nil
	}
	baseURL, err := spawnServer(ctx, workDir)
	if err != nil {
		return "", err
	}
	if err := saveServerInfo(path, ServerInfo{BaseURL: baseURL}); err != nil {
		return "", err
	}
	return baseURL, nil
}

func resolveRunningServer(ctx context.Context, path, fallbackURL string) (string, bool) {
	if info, err := loadServerInfo(path); err == nil && serverHealthy(ctx, info.BaseURL) {
		return info.BaseURL, true
	}
	if fallbackURL != "" && serverHealthy(ctx, fallbackURL) {
		return fallbackURL, true
	}
	return "", false
}

func serverHealthy(ctx context.Context, baseURL string) bool {
	healthCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	return New(baseURL).Health(healthCtx) == nil
}

func loadServerInfo(path string) (ServerInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ServerInfo{}, err
	}
	var info ServerInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return ServerInfo{}, err
	}
	if strings.TrimSpace(info.BaseURL) == "" {
		return ServerInfo{}, errors.New("codexserve: empty base URL in server info")
	}
	return info, nil
}

func saveServerInfo(path string, info ServerInfo) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func spawnServer(ctx context.Context, workDir string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("codexserve: locate Agent Comms executable: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	command := exec.Command(executable, "--project", workDir, "codex", "serve", "--listen", DefaultServeAddress)
	command.Dir = workDir
	command.SysProcAttr = detachedProcAttr()
	command.Stdout = nil
	command.Stderr = nil
	if err := command.Start(); err != nil {
		return "", fmt.Errorf("codexserve: start broker: %w", err)
	}
	return waitUntilHealthy(ctx, DefaultServeBaseURL())
}

func waitUntilHealthy(ctx context.Context, baseURL string) (string, error) {
	deadline := time.Now().Add(serverStartupTimeout)
	for time.Now().Before(deadline) {
		if serverHealthy(ctx, baseURL) {
			return baseURL, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("codexserve: %s did not become healthy within %s", baseURL, serverStartupTimeout)
}
