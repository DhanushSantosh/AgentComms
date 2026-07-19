package authority

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const (
	outboxBatchSize    = 100
	outboxPollInterval = 100 * time.Millisecond
	minOutboxBackoff   = 100 * time.Millisecond
	maxOutboxBackoff   = time.Minute
)

type OutboxStats struct {
	Pending uint64     `json:"pending"`
	Oldest  *time.Time `json:"oldest,omitempty"`
}

func (e *Engine) RunOutbox(ctx context.Context) error {
	retryDelay := minOutboxBackoff
	timer := time.NewTimer(outboxPollInterval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			_, err := e.publishOutboxBatch(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				timer.Reset(retryDelay)
				retryDelay = min(retryDelay*2, maxOutboxBackoff)
				continue
			}
			retryDelay = minOutboxBackoff
			timer.Reset(outboxPollInterval)
		}
	}
}

func (e *Engine) publishOutboxBatch(ctx context.Context) (int, error) {
	tx, err := e.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, unavailable(err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT project_id,sequence FROM outbox
		WHERE published_at IS NULL AND next_attempt_at<=CURRENT_TIMESTAMP
		ORDER BY next_attempt_at LIMIT $1 FOR UPDATE SKIP LOCKED`, outboxBatchSize)
	if err != nil {
		return 0, unavailable(err)
	}
	type item struct {
		projectID string
		sequence  uint64
	}
	var items []item
	for rows.Next() {
		var value item
		if err = rows.Scan(&value.projectID, &value.sequence); err != nil {
			_ = rows.Close()
			return 0, unavailable(err)
		}
		items = append(items, value)
	}
	if err = rows.Close(); err != nil {
		return 0, unavailable(err)
	}
	for _, value := range items {
		payload, _ := json.Marshal(map[string]any{"project_id": value.projectID, "sequence": value.sequence})
		if _, err = tx.ExecContext(ctx, `SELECT pg_notify('agent_comms_events',$1)`, string(payload)); err != nil {
			return 0, unavailable(err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE outbox SET published_at=CURRENT_TIMESTAMP,attempts=attempts+1
			WHERE project_id=$1 AND sequence=$2`, value.projectID, value.sequence); err != nil {
			return 0, unavailable(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, unavailable(err)
	}
	return len(items), nil
}

func (e *Engine) OutboxStats(ctx context.Context) (OutboxStats, error) {
	var stats OutboxStats
	var oldest sql.NullTime
	if err := e.db.QueryRowContext(ctx, `SELECT COUNT(*),MIN(next_attempt_at) FROM outbox WHERE published_at IS NULL`).
		Scan(&stats.Pending, &oldest); err != nil {
		return stats, unavailable(err)
	}
	if oldest.Valid {
		stats.Oldest = &oldest.Time
	}
	return stats, nil
}
