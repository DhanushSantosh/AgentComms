package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/authority"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
)

const (
	defaultAddress          = "127.0.0.1:8787"
	serverReadTimeout       = 15 * time.Second
	serverWriteTimeout      = 30 * time.Second
	serverIdleTimeout       = 60 * time.Second
	serverShutdownTimeout   = 20 * time.Second
	defaultStatementTimeout = 5 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("authority stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	databaseURL := os.Getenv("AGENT_COMMS_DATABASE_URL")
	production := strings.EqualFold(os.Getenv("AGENT_COMMS_ENV"), "production")
	signer, ephemeral, err := loadSigner(production)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if ephemeral {
		logger.Warn("using ephemeral development signing key", "public_key", signer.PublicKey())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	engine, err := authority.Open(ctx, authority.Config{
		DatabaseURL: databaseURL, MaxConnections: envInt("AGENT_COMMS_DB_MAX_CONNECTIONS", 32),
		MinConnections:   envInt("AGENT_COMMS_DB_MIN_CONNECTIONS", 4),
		StatementTimeout: defaultStatementTimeout, Production: production,
	}, signer)
	if err != nil {
		return err
	}
	defer engine.Close()
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	workerFailures := make(chan error, 1)
	go func() { workerFailures <- engine.RunOutbox(workerCtx) }()

	address := os.Getenv("AGENT_COMMS_LISTEN")
	if address == "" {
		address = defaultAddress
	}
	server := &http.Server{
		Addr: address, Handler: authority.NewHTTPServer(engine, authority.HTTPConfig{
			MaxInFlight:    envInt("AGENT_COMMS_MAX_IN_FLIGHT", 256),
			MigrationToken: strings.TrimSpace(os.Getenv("AGENT_COMMS_MIGRATION_TOKEN")),
			Logger:         logger,
		}).Handler(),
		ReadHeaderTimeout: serverReadTimeout, ReadTimeout: serverReadTimeout,
		WriteTimeout: serverWriteTimeout, IdleTimeout: serverIdleTimeout,
	}
	failures := make(chan error, 1)
	go func() {
		logger.Info("authority listening", "address", address)
		certificate := os.Getenv("AGENT_COMMS_TLS_CERT")
		key := os.Getenv("AGENT_COMMS_TLS_KEY")
		if certificate != "" || key != "" {
			if certificate == "" || key == "" {
				failures <- errors.New("both AGENT_COMMS_TLS_CERT and AGENT_COMMS_TLS_KEY are required")
				return
			}
			failures <- server.ListenAndServeTLS(certificate, key)
			return
		}
		if production {
			failures <- errors.New("production mode requires TLS configuration")
			return
		}
		failures <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err = <-failures:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case received := <-signals:
		logger.Info("authority shutting down", "signal", received.String())
	case err = <-workerFailures:
		return fmt.Errorf("outbox worker: %w", err)
	}
	stopWorker()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
	defer shutdownCancel()
	return server.Shutdown(shutdownCtx)
}

func loadSigner(production bool) (*controlplane.Signer, bool, error) {
	privateKey := strings.TrimSpace(os.Getenv("AGENT_COMMS_SERVICE_PRIVATE_KEY"))
	if path := strings.TrimSpace(os.Getenv("AGENT_COMMS_SERVICE_KEY_FILE")); path != "" {
		if privateKey != "" {
			return nil, false, errors.New("configure only one service signing key source")
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, false, fmt.Errorf("inspect service signing key: %w", err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			return nil, false, errors.New("service signing key file must not be accessible by group or other users")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, false, fmt.Errorf("read service signing key: %w", err)
		}
		privateKey = strings.TrimSpace(string(raw))
	}
	if privateKey != "" {
		signer, err := controlplane.NewSigner(privateKey)
		return signer, false, err
	}
	if production {
		return nil, false, errors.New("production mode requires AGENT_COMMS_SERVICE_KEY_FILE or AGENT_COMMS_SERVICE_PRIVATE_KEY")
	}
	signer, err := controlplane.GenerateSigner()
	return signer, true, err
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
