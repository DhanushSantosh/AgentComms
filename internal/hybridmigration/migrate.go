package hybridmigration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/authority"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/remote"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

type Config struct {
	ProjectRoot      string
	AuthorityURL     string
	ServicePublicKey string
	DaemonEndpoint   string
	MigrationToken   string
	BatchSize        int
}

type Result struct {
	ProjectID      string                `json:"project_id"`
	ImportedEvents uint64                `json:"imported_events"`
	ProjectionHash string                `json:"projection_hash"`
	State          string                `json:"state"`
	Receipt        *controlplane.Receipt `json:"receipt,omitempty"`
}

func Migrate(ctx context.Context, cfg Config) (Result, error) {
	if cfg.ProjectRoot == "" || cfg.AuthorityURL == "" || cfg.ServicePublicKey == "" ||
		cfg.DaemonEndpoint == "" || cfg.MigrationToken == "" {
		return Result{}, errors.New("project root, authority URL, service public key, daemon endpoint, and migration token are required")
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = authority.MaxImportBatchEvents
	}
	if batchSize > authority.MaxImportBatchEvents {
		return Result{}, fmt.Errorf("batch size cannot exceed %d", authority.MaxImportBatchEvents)
	}
	legacy := store.Open(cfg.ProjectRoot)
	if err := legacy.Verify(); err != nil {
		return Result{}, fmt.Errorf("verify legacy runtime: %w", err)
	}
	runtimeConfig, err := legacy.Config()
	if err != nil {
		return Result{}, err
	}
	if runtimeConfig.RuntimeMode == "service" || runtimeConfig.LegacyReadOnly {
		return Result{}, errors.New("project is already in service mode")
	}
	events, rawEvents, err := readLegacyEvents(cfg.ProjectRoot)
	if err != nil {
		return Result{}, err
	}
	if len(events) == 0 {
		return Result{}, errors.New("legacy runtime has no events")
	}
	serviceState, err := service.New(cfg.ProjectRoot).State()
	if err != nil {
		return Result{}, err
	}
	projectionHash := service.ProjectionHash(serviceState)
	client, err := remote.NewMigration(cfg.AuthorityURL, cfg.MigrationToken, 30*time.Second)
	if err != nil {
		return Result{}, err
	}
	if err = retry(ctx, func() error {
		return client.CreateProject(ctx, runtimeConfig.ProjectID, runtimeConfig.Owner)
	}); err != nil {
		return Result{}, err
	}
	start := authority.LegacyImportStart{
		ProjectID: runtimeConfig.ProjectID, LegacyHeadHash: events[len(events)-1].Hash,
		LegacyGitCommit: legacy.Head(), ExpectedEvents: uint64(len(events)),
	}
	var status authority.LegacyImportStatus
	if err = retry(ctx, func() error {
		return client.BeginLegacyImport(ctx, runtimeConfig.ProjectID, start, &status)
	}); err != nil {
		return Result{}, err
	}
	for status.ImportedSequence < uint64(len(rawEvents)) {
		from := status.ImportedSequence
		end := min(from+uint64(batchSize), uint64(len(rawEvents)))
		batch := authority.LegacyImportBatch{
			FromSequence: from + 1,
			Events:       append([]json.RawMessage(nil), rawEvents[from:end]...),
		}
		if err = retry(ctx, func() error {
			return client.ImportLegacyBatch(ctx, runtimeConfig.ProjectID, batch, &status)
		}); err != nil {
			return Result{}, err
		}
	}
	if err = retry(ctx, func() error {
		return client.FinalizeLegacyImport(ctx, runtimeConfig.ProjectID,
			map[string]string{"projection_hash": projectionHash}, &status)
	}); err != nil {
		return Result{}, err
	}
	if status.State != "READY" || status.Receipt == nil {
		return Result{}, errors.New("authority did not return a ready import receipt")
	}
	if !controlplane.VerifyReceipt(*status.Receipt, cfg.ServicePublicKey) {
		return Result{}, errors.New("authority import receipt signature is invalid")
	}
	if err = legacy.ActivateServiceMode(cfg.AuthorityURL, cfg.ServicePublicKey, cfg.DaemonEndpoint, status.Receipt); err != nil {
		return Result{}, fmt.Errorf("authority import succeeded but local cutover failed: %w", err)
	}
	return Result{
		ProjectID: runtimeConfig.ProjectID, ImportedEvents: status.ImportedSequence,
		ProjectionHash: projectionHash, State: "ACTIVATED", Receipt: status.Receipt,
	}, nil
}

func retry(ctx context.Context, operation func() error) error {
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		if last = operation(); last == nil {
			return nil
		}
		var controlErr *controlplane.Error
		if !errors.As(last, &controlErr) ||
			(controlErr.Code != controlplane.CodeConflict && controlErr.Code != controlplane.CodeUnavailable &&
				controlErr.Code != controlplane.CodeRateLimited) {
			return last
		}
		delay := time.Duration(100*(1<<attempt)+rand.IntN(100)) * time.Millisecond
		if controlErr.RetryAfter > delay {
			delay = controlErr.RetryAfter
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return last
}

func readLegacyEvents(root string) ([]controlplane.Event, []json.RawMessage, error) {
	paths, err := filepath.Glob(filepath.Join(root, store.Runtime, "events", "*.json"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(paths)
	events := make([]controlplane.Event, 0, len(paths))
	rawEvents := make([]json.RawMessage, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		var legacyEvent struct {
			Sequence uint64 `json:"sequence"`
			Hash     string `json:"hash"`
		}
		if err = json.Unmarshal(raw, &legacyEvent); err != nil {
			return nil, nil, err
		}
		events = append(events, controlplane.Event{Sequence: legacyEvent.Sequence, Hash: legacyEvent.Hash})
		rawEvents = append(rawEvents, json.RawMessage(append([]byte(nil), raw...)))
	}
	return events, rawEvents, nil
}
