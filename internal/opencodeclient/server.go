package opencodeclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ServerInfo is the locally-cached record of the persistent opencode serve
// instance a project's OpenCode-driven worker uses. Like sessionbind, this
// is local, non-authoritative routing metadata — never part of the signed
// project event chain — that lets repeated invocations, and a user's
// browser, find the same long-lived server instead of each spawning a new
// one.
type ServerInfo struct {
	BaseURL string `json:"base_url"`
}

// ServerInfoPath returns the local tracking file path for a project root.
func ServerInfoPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".agent-comms", "cache", "opencode-server.json")
}

// EnsureServer returns a running opencode serve instance's base URL for
// this project, reusing a healthy one recorded at ServerInfoPath or
// spawning a fresh one if none exists or the recorded one no longer
// responds. Concurrent-safe in the sense that a stale or missing record
// always results in exactly one new spawn per call, matching the existing
// per-project daemon's own auto-spawn convention.
func EnsureServer(ctx context.Context, projectRoot, workDir string) (string, error) {
	path := ServerInfoPath(projectRoot)
	if info, err := loadServerInfo(path); err == nil {
		healthCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		client := New(info.BaseURL)
		healthErr := client.Health(healthCtx)
		cancel()
		if healthErr == nil {
			return info.BaseURL, nil
		}
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
		return ServerInfo{}, errors.New("opencodeclient: empty base URL in server info")
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

const serverStartupTimeout = 30 * time.Second

// spawnServer starts a new, detached `opencode serve` instance bound to
// loopback on an OS-assigned port, parses its own log line reporting the
// address it bound, and returns once that address is confirmed reachable.
// The process is not tied to the caller's context or lifetime — it must
// keep running after this call (and the invocation that triggered it)
// returns, since it's meant to be a stable, repeatedly-reusable, browser-
// watchable server, not a per-invocation subprocess.
func spawnServer(ctx context.Context, workDir string) (string, error) {
	cmd := exec.Command("opencode", "serve", "--hostname", "127.0.0.1", "--port", "0", "--pure")
	cmd.Dir = workDir
	cmd.SysProcAttr = detachedProcAttr()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("opencodeclient: open stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("opencodeclient: start opencode serve: %w", err)
	}

	urlCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if url, ok := parseListeningURL(scanner.Text()); ok {
				urlCh <- url
				return
			}
		}
		errCh <- fmt.Errorf("opencodeclient: opencode serve exited before reporting a listen address")
	}()

	deadline := time.After(serverStartupTimeout)
	select {
	case baseURL := <-urlCh:
		return waitUntilHealthy(ctx, baseURL)
	case err := <-errCh:
		return "", err
	case <-deadline:
		return "", fmt.Errorf("opencodeclient: opencode serve did not report a listen address within %s", serverStartupTimeout)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// parseListeningURL extracts the base URL from opencode serve's own log
// line, e.g. "opencode server listening on http://127.0.0.1:4098".
func parseListeningURL(line string) (string, bool) {
	const marker = "listening on "
	index := strings.Index(line, marker)
	if index < 0 {
		return "", false
	}
	url := strings.TrimSpace(line[index+len(marker):])
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", false
	}
	return url, true
}

func waitUntilHealthy(ctx context.Context, baseURL string) (string, error) {
	client := New(baseURL)
	deadline := time.Now().Add(serverStartupTimeout)
	for time.Now().Before(deadline) {
		healthCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := client.Health(healthCtx)
		cancel()
		if err == nil {
			return baseURL, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("opencodeclient: %s did not become healthy within %s", baseURL, serverStartupTimeout)
}
