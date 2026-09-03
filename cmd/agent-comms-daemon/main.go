package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/DhanushSantosh/AgentComms/internal/daemon"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
)

func main() {
	if err := run(); err != nil {
		slog.Error("daemon stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	authorityURL := strings.TrimSpace(os.Getenv("AGENT_COMMS_AUTHORITY_URL"))
	serverPublicKey := strings.TrimSpace(os.Getenv("AGENT_COMMS_SERVICE_PUBLIC_KEY"))
	if authorityURL == "" || serverPublicKey == "" {
		return errors.New("AGENT_COMMS_AUTHORITY_URL and AGENT_COMMS_SERVICE_PUBLIC_KEY are required")
	}
	configDir, err := identity.ConfigDir()
	if err != nil {
		return err
	}
	cachePath := os.Getenv("AGENT_COMMS_CACHE_PATH")
	if cachePath == "" {
		cachePath = filepath.Join(configDir, "cache.db")
	}
	endpoint := os.Getenv("AGENT_COMMS_DAEMON_ENDPOINT")
	if endpoint == "" {
		endpoint = defaultEndpoint(configDir)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-signals
		cancel()
	}()
	slog.Info("daemon listening", "endpoint", endpoint)
	return daemon.Run(ctx, daemon.RunConfig{
		AuthorityURL: authorityURL, ServicePublicKey: serverPublicKey,
		AuthorityToken: strings.TrimSpace(os.Getenv("AGENT_COMMS_AUTHORITY_TOKEN")),
		CachePath:      cachePath, Endpoint: endpoint,
		ConnectorConfigPath: strings.TrimSpace(os.Getenv("AGENT_COMMS_CONNECTOR_CONFIG")),
		RuntimeMode:         "service", ProjectID: "*",
	})
}

func defaultEndpoint(configDir string) string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\agent-comms`
	}
	return filepath.Join(configDir, "daemon.sock")
}
