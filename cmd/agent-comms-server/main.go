package main

import (
	"context"
	"encoding/json"
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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
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
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		return runMigrationCommand(databaseURL, os.Args[2:])
	}
	production := strings.EqualFold(os.Getenv("AGENT_COMMS_ENV"), "production")
	authorityToken := strings.TrimSpace(os.Getenv("AGENT_COMMS_AUTHORITY_TOKEN"))
	if err := validateRuntimeSecrets(production, authorityToken); err != nil {
		return err
	}
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
			MaxInFlight: envInt("AGENT_COMMS_MAX_IN_FLIGHT", 256),
			BearerToken: authorityToken,
			Logger:      logger,
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

func validateRuntimeSecrets(production bool, authorityToken string) error {
	if production && strings.TrimSpace(authorityToken) == "" {
		return errors.New("production mode requires AGENT_COMMS_AUTHORITY_TOKEN")
	}
	return nil
}

// parseMigrationCommand validates the migrate subcommand's arguments and
// flags with no I/O, so this gating logic (arg arity, and --yes plus
// --allow-disruptive both being required for apply) is unit-testable
// without a live Postgres instance. It returns the resolved subcommand
// ("plan" or "apply") on success.
func parseMigrationCommand(databaseURL string, args []string) (string, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return "", errors.New("AGENT_COMMS_DATABASE_URL is required")
	}
	if len(args) != 1 && !(len(args) == 3 && args[0] == "apply") {
		return "", errors.New("usage: agent-comms-server migrate plan | migrate apply --yes --allow-disruptive")
	}
	switch args[0] {
	case "plan":
		return "plan", nil
	case "apply":
		flags := map[string]bool{}
		for _, flag := range args[1:] {
			flags[flag] = true
		}
		if !flags["--yes"] || !flags["--allow-disruptive"] {
			return "", errors.New("migrate apply requires --yes and --allow-disruptive")
		}
		return "apply", nil
	default:
		return "", errors.New("migration command must be plan or apply")
	}
}

func runMigrationCommand(databaseURL string, args []string) error {
	command, err := parseMigrationCommand(databaseURL, args)
	if err != nil {
		return err
	}
	connectionConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse PostgreSQL configuration: %w", err)
	}
	db := stdlib.OpenDB(*connectionConfig)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		return err
	}
	switch command {
	case "plan":
		plan, planErr := authority.SchemaPlan(ctx, db)
		if planErr != nil {
			return planErr
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"schema_version": authority.CurrentSchemaVersion, "migrations": plan,
		})
	case "apply":
		if err = authority.ApplySchema(ctx, db, true); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"schema_version": authority.CurrentSchemaVersion, "applied": true,
		})
	}
	return nil
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
