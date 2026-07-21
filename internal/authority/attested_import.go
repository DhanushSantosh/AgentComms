package authority

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

func (e *Engine) BeginAttestedImport(ctx context.Context, request controlplane.AttestedImportStart) (LegacyImportStatus, error) {
	if request.ProjectID == "" || request.SourcePublicKey == "" || request.SourceHeadHash == "" || request.ExpectedEvents == 0 {
		return LegacyImportStatus{}, &controlplane.Error{
			Code: controlplane.CodeValidation, Message: "project, source public key, source head, and expected event count are required",
		}
	}
	tx, err := e.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	defer func() { _ = tx.Rollback() }()
	var head uint64
	if err = tx.QueryRowContext(ctx, `SELECT head_sequence FROM projects WHERE project_id=$1 FOR UPDATE`,
		request.ProjectID).Scan(&head); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	if head != 0 {
		return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeConflict, Message: "attested import requires an empty authoritative project"}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO legacy_imports
		(project_id,legacy_head_hash,legacy_git_commit,expected_events,state,source_kind,source_public_key,source_head_hash)
		VALUES ($1,$2,'personal-sqlite',$3,'PREPARED','personal',$4,'')
		ON CONFLICT (project_id) DO NOTHING`,
		request.ProjectID, request.SourceHeadHash, request.ExpectedEvents, request.SourcePublicKey); err != nil {
		return LegacyImportStatus{}, conflictOrUnavailable(err)
	}
	if err = tx.Commit(); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	return e.LegacyImportStatus(ctx, request.ProjectID)
}

func (e *Engine) ImportAttestedBatch(ctx context.Context, projectID string, batch controlplane.AttestedImportBatch) (LegacyImportStatus, error) {
	if len(batch.Records) == 0 || len(batch.Records) > MaxImportBatchEvents {
		return LegacyImportStatus{}, &controlplane.Error{
			Code:    controlplane.CodeValidation,
			Message: fmt.Sprintf("import batch must contain 1 to %d records", MaxImportBatchEvents),
		}
	}
	tx, err := e.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	defer func() { _ = tx.Rollback() }()
	var imported, expected uint64
	var stateName, sourceKey, sourceHead, sourceKind string
	if err = tx.QueryRowContext(ctx, `SELECT imported_sequence,expected_events,state,source_public_key,source_head_hash,source_kind
		FROM legacy_imports WHERE project_id=$1 FOR UPDATE`, projectID).
		Scan(&imported, &expected, &stateName, &sourceKey, &sourceHead, &sourceKind); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	if sourceKind != "personal" {
		return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeConflict, Message: "project import is not an attested personal import"}
	}
	if stateName == "ACTIVATED" {
		return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeConflict, Message: "attested import is already activated"}
	}
	if batch.FromSequence != imported+1 {
		if batch.FromSequence <= imported {
			return LegacyImportStatus{ProjectID: projectID, ImportedSequence: imported, ExpectedEvents: expected, State: stateName}, nil
		}
		return LegacyImportStatus{}, &controlplane.Error{
			Code: controlplane.CodeStalePrecondition, Message: fmt.Sprintf("next import sequence is %d", imported+1),
		}
	}
	state, err := loadState(ctx, tx, projectID)
	if err != nil {
		return LegacyImportStatus{}, err
	}
	var authoritySequence uint64
	var authorityHead string
	if err = tx.QueryRowContext(ctx, `SELECT head_sequence,head_hash FROM projects WHERE project_id=$1 FOR UPDATE`,
		projectID).Scan(&authoritySequence, &authorityHead); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	for _, record := range batch.Records {
		sourceEvent := record.Event
		if sourceEvent.ProjectID != projectID || sourceEvent.Sequence != imported+1 ||
			sourceEvent.PreviousHash != sourceHead {
			return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "attested source stream is discontinuous"}
		}
		hash, hashErr := controlplane.HashEvent(sourceEvent)
		if hashErr != nil || hash != sourceEvent.Hash {
			return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "attested source event hash is invalid"}
		}
		if record.Receipt.ProjectID != projectID || record.Receipt.Sequence != sourceEvent.Sequence ||
			record.Receipt.EventID != sourceEvent.ID || record.Receipt.EventHash != sourceEvent.Hash ||
			record.Receipt.ActorIntentHash != sourceEvent.ActorIntentHash ||
			!controlplane.VerifyReceipt(record.Receipt, sourceKey) {
			return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "attested source receipt is invalid"}
		}
		before := cloneState(state)
		if err = service.ApplyEvent(&state, model.Event{
			SchemaVersion: model.SchemaVersion, PayloadVersion: 1, ID: sourceEvent.ID,
			Sequence: sourceEvent.Sequence, Time: sourceEvent.Time, Actor: sourceEvent.Actor,
			Type: sourceEvent.Type, EntityID: sourceEvent.EntityID, Data: sourceEvent.Payload,
			PreviousHash: sourceEvent.PreviousHash, Hash: sourceEvent.Hash,
		}); err != nil {
			return LegacyImportStatus{}, err
		}
		authoritySequence++
		event := controlplane.Event{
			ProjectID: projectID, Sequence: authoritySequence,
			ID:   fmt.Sprintf("personal-%020d", sourceEvent.Sequence),
			Time: sourceEvent.Time, Actor: sourceEvent.Actor, Type: sourceEvent.Type,
			EntityID: sourceEvent.EntityID, Payload: sourceEvent.Payload,
			PreviousHash: authorityHead, ActorIntentHash: sourceEvent.Hash,
			IdempotencyKey: "personal:" + sourceEvent.ID, Legacy: true,
		}
		event.Hash, err = controlplane.HashEvent(event)
		if err != nil {
			return LegacyImportStatus{}, err
		}
		receipt := controlplane.Receipt{
			ProjectID: projectID, Sequence: authoritySequence, EventID: event.ID,
			EventHash: event.Hash, ActorIntentHash: sourceEvent.Hash, CommittedAt: e.now(),
		}
		if err = e.signer.SignReceipt(&receipt); err != nil {
			return LegacyImportStatus{}, err
		}
		if err = persistProjectionChanges(ctx, tx, projectID, authoritySequence, before, state); err != nil {
			return LegacyImportStatus{}, err
		}
		receiptJSON, _ := json.Marshal(receipt)
		eventJSON, _ := json.Marshal(event)
		sourceJSON, _ := json.Marshal(record)
		if _, err = tx.ExecContext(ctx, `INSERT INTO events
			(project_id,sequence,event_id,event_time,actor_id,event_type,entity_id,payload,previous_hash,
			 event_hash,actor_intent_hash,actor_signature,idempotency_key,receipt,legacy_bytes)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'',$12,$13,$14)`,
			projectID, authoritySequence, event.ID, event.Time, event.Actor, event.Type, event.EntityID,
			event.Payload, event.PreviousHash, event.Hash, event.ActorIntentHash,
			event.IdempotencyKey, receiptJSON, sourceJSON); err != nil {
			return LegacyImportStatus{}, conflictOrUnavailable(err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO outbox (project_id,sequence,event) VALUES ($1,$2,$3)`,
			projectID, authoritySequence, eventJSON); err != nil {
			return LegacyImportStatus{}, unavailable(err)
		}
		imported++
		sourceHead = sourceEvent.Hash
		authorityHead = event.Hash
	}
	if imported > expected {
		return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeValidation, Message: "import exceeds expected event count"}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE projects SET head_sequence=$2,head_hash=$3,updated_at=$4 WHERE project_id=$1`,
		projectID, authoritySequence, authorityHead, e.now()); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE legacy_imports SET imported_sequence=$2,source_head_hash=$3,
		state='IMPORTING',updated_at=$4 WHERE project_id=$1`, projectID, imported, sourceHead, e.now()); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	if err = tx.Commit(); err != nil {
		return LegacyImportStatus{}, conflictOrUnavailable(err)
	}
	return LegacyImportStatus{ProjectID: projectID, ImportedSequence: imported, ExpectedEvents: expected, State: "IMPORTING"}, nil
}

func (e *Engine) FinalizeAttestedImport(ctx context.Context, projectID, expectedProjectionHash string) (LegacyImportStatus, error) {
	tx, err := e.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	defer func() { _ = tx.Rollback() }()
	var imported, expected uint64
	var expectedHead, actualHead, sourceKind string
	if err = tx.QueryRowContext(ctx, `SELECT imported_sequence,expected_events,legacy_head_hash,source_head_hash,source_kind
		FROM legacy_imports WHERE project_id=$1 FOR UPDATE`, projectID).
		Scan(&imported, &expected, &expectedHead, &actualHead, &sourceKind); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	if sourceKind != "personal" || imported != expected || actualHead != expectedHead {
		return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeStalePrecondition, Message: "attested import is incomplete"}
	}
	state, err := loadState(ctx, tx, projectID)
	if err != nil {
		return LegacyImportStatus{}, err
	}
	actualProjectionHash := projectionHash(state)
	if expectedProjectionHash == "" || actualProjectionHash != expectedProjectionHash {
		return LegacyImportStatus{}, &controlplane.Error{
			Code:    controlplane.CodeConflict,
			Message: fmt.Sprintf("projection hash mismatch: authority=%s importer=%s", actualProjectionHash, expectedProjectionHash),
		}
	}
	var receiptJSON []byte
	if err = tx.QueryRowContext(ctx, `SELECT receipt FROM events WHERE project_id=$1 ORDER BY sequence DESC LIMIT 1`,
		projectID).Scan(&receiptJSON); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	var receipt controlplane.Receipt
	if err = json.Unmarshal(receiptJSON, &receipt); err != nil {
		return LegacyImportStatus{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE legacy_imports SET projection_hash=$2,state='ACTIVATED',
		receipt=$3,updated_at=$4 WHERE project_id=$1`, projectID, actualProjectionHash, receiptJSON, e.now()); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	if err = tx.Commit(); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	return LegacyImportStatus{
		ProjectID: projectID, ImportedSequence: imported, ExpectedEvents: expected,
		State: "READY", Receipt: &receipt,
	}, nil
}
