package authority

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

const MaxImportBatchEvents = 100

type LegacyImportStart struct {
	ProjectID       string `json:"project_id"`
	LegacyHeadHash  string `json:"legacy_head_hash"`
	LegacyGitCommit string `json:"legacy_git_commit"`
	ExpectedEvents  uint64 `json:"expected_events"`
}

type LegacyImportBatch struct {
	FromSequence uint64            `json:"from_sequence"`
	Events       []json.RawMessage `json:"events"`
}

type LegacyImportStatus struct {
	ProjectID        string                `json:"project_id"`
	ImportedSequence uint64                `json:"imported_sequence"`
	ExpectedEvents   uint64                `json:"expected_events"`
	State            string                `json:"state"`
	Receipt          *controlplane.Receipt `json:"receipt,omitempty"`
}

func (e *Engine) BeginLegacyImport(ctx context.Context, request LegacyImportStart) (LegacyImportStatus, error) {
	if request.ProjectID == "" || request.LegacyHeadHash == "" || request.LegacyGitCommit == "" || request.ExpectedEvents == 0 {
		return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeValidation, Message: "project, legacy head, Git commit, and expected event count are required"}
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
		return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeConflict, Message: "legacy import requires an empty authoritative project"}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO legacy_imports
		(project_id,legacy_head_hash,legacy_git_commit,expected_events,state)
		VALUES ($1,$2,$3,$4,'PREPARED')
		ON CONFLICT (project_id) DO NOTHING`,
		request.ProjectID, request.LegacyHeadHash, request.LegacyGitCommit, request.ExpectedEvents); err != nil {
		return LegacyImportStatus{}, conflictOrUnavailable(err)
	}
	if err = tx.Commit(); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	return e.LegacyImportStatus(ctx, request.ProjectID)
}

func (e *Engine) ImportLegacyBatch(ctx context.Context, projectID string, batch LegacyImportBatch) (LegacyImportStatus, error) {
	if len(batch.Events) == 0 || len(batch.Events) > MaxImportBatchEvents {
		return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeValidation, Message: fmt.Sprintf("import batch must contain 1 to %d events", MaxImportBatchEvents)}
	}
	tx, err := e.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	defer func() { _ = tx.Rollback() }()
	var imported, expected uint64
	var stateName string
	if err = tx.QueryRowContext(ctx, `SELECT imported_sequence,expected_events,state FROM legacy_imports
		WHERE project_id=$1 FOR UPDATE`, projectID).Scan(&imported, &expected, &stateName); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	if stateName == "ACTIVATED" {
		return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeConflict, Message: "legacy import is already activated"}
	}
	if batch.FromSequence != imported+1 {
		if batch.FromSequence <= imported {
			return LegacyImportStatus{ProjectID: projectID, ImportedSequence: imported, ExpectedEvents: expected, State: stateName}, nil
		}
		return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeStalePrecondition, Message: fmt.Sprintf("next import sequence is %d", imported+1)}
	}
	legacyEvents, err := storedLegacyEvents(ctx, tx, projectID)
	if err != nil {
		return LegacyImportStatus{}, err
	}
	newEvents := make([]model.Event, 0, len(batch.Events))
	for _, raw := range batch.Events {
		var event model.Event
		if err = json.Unmarshal(raw, &event); err != nil {
			return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeValidation, Message: "invalid legacy event: " + err.Error()}
		}
		newEvents = append(newEvents, event)
		legacyEvents = append(legacyEvents, event)
	}
	if err = verifyLegacyChain(legacyEvents); err != nil {
		return LegacyImportStatus{}, err
	}
	if uint64(len(legacyEvents)) > expected {
		return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeValidation, Message: "import exceeds expected event count"}
	}
	state, err := loadState(ctx, tx, projectID)
	if err != nil {
		return LegacyImportStatus{}, err
	}
	var previousAuthorityHash string
	var authoritySequence uint64
	if err = tx.QueryRowContext(ctx, `SELECT head_sequence,head_hash FROM projects WHERE project_id=$1 FOR UPDATE`,
		projectID).Scan(&authoritySequence, &previousAuthorityHash); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	for index, legacy := range newEvents {
		raw := batch.Events[index]
		sequence := authoritySequence + 1
		event := controlplane.Event{
			ProjectID: projectID, Sequence: sequence, ID: fmt.Sprintf("legacy-%020d", legacy.Sequence),
			Time: legacy.Time.Truncate(time.Microsecond), Actor: legacy.Actor, Type: legacy.Type, EntityID: legacy.EntityID,
			Payload: legacy.Data, PreviousHash: previousAuthorityHash, ActorIntentHash: legacy.Hash,
			IdempotencyKey: "legacy:" + legacy.ID, Legacy: true,
		}
		event.Hash, err = controlplane.HashEvent(event)
		if err != nil {
			return LegacyImportStatus{}, err
		}
		receipt := controlplane.Receipt{
			ProjectID: projectID, Sequence: sequence, EventID: event.ID,
			EventHash: event.Hash, ActorIntentHash: legacy.Hash, CommittedAt: e.now(),
		}
		if err = e.signer.SignReceipt(&receipt); err != nil {
			return LegacyImportStatus{}, err
		}
		before := cloneState(state)
		if err = service.ApplyEvent(&state, legacy); err != nil {
			return LegacyImportStatus{}, err
		}
		if err = persistProjectionChanges(ctx, tx, projectID, sequence, before, state); err != nil {
			return LegacyImportStatus{}, err
		}
		receiptJSON, _ := json.Marshal(receipt)
		eventJSON, _ := json.Marshal(event)
		if _, err = tx.ExecContext(ctx, `INSERT INTO events
			(project_id,sequence,event_id,event_time,actor_id,event_type,entity_id,payload,previous_hash,
			 event_hash,actor_intent_hash,actor_signature,idempotency_key,receipt,legacy_bytes)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
			projectID, sequence, event.ID, event.Time, event.Actor, event.Type, event.EntityID,
			event.Payload, event.PreviousHash, event.Hash, event.ActorIntentHash, legacy.Signature,
			event.IdempotencyKey, receiptJSON, []byte(raw)); err != nil {
			return LegacyImportStatus{}, conflictOrUnavailable(err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO outbox (project_id,sequence,event) VALUES ($1,$2,$3)`,
			projectID, sequence, eventJSON); err != nil {
			return LegacyImportStatus{}, unavailable(err)
		}
		authoritySequence = sequence
		previousAuthorityHash = event.Hash
	}
	imported = uint64(len(legacyEvents))
	if _, err = tx.ExecContext(ctx, `UPDATE projects SET head_sequence=$2,head_hash=$3,updated_at=$4 WHERE project_id=$1`,
		projectID, authoritySequence, previousAuthorityHash, e.now()); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE legacy_imports SET imported_sequence=$2,state='IMPORTING',updated_at=$3 WHERE project_id=$1`,
		projectID, imported, e.now()); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	if err = tx.Commit(); err != nil {
		return LegacyImportStatus{}, conflictOrUnavailable(err)
	}
	return LegacyImportStatus{ProjectID: projectID, ImportedSequence: imported, ExpectedEvents: expected, State: "IMPORTING"}, nil
}

func (e *Engine) FinalizeLegacyImport(ctx context.Context, projectID, expectedProjectionHash string) (LegacyImportStatus, error) {
	tx, err := e.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	defer func() { _ = tx.Rollback() }()
	var imported, expected uint64
	var legacyHead string
	if err = tx.QueryRowContext(ctx, `SELECT imported_sequence,expected_events,legacy_head_hash
		FROM legacy_imports WHERE project_id=$1 FOR UPDATE`, projectID).Scan(&imported, &expected, &legacyHead); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	if imported != expected {
		return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeStalePrecondition, Message: fmt.Sprintf("imported %d of %d events", imported, expected)}
	}
	legacyEvents, err := storedLegacyEvents(ctx, tx, projectID)
	if err != nil {
		return LegacyImportStatus{}, err
	}
	if len(legacyEvents) == 0 || legacyEvents[len(legacyEvents)-1].Hash != legacyHead {
		return LegacyImportStatus{}, &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "imported legacy head does not match the declared head"}
	}
	if err = verifyLegacyChain(legacyEvents); err != nil {
		return LegacyImportStatus{}, err
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
	var sequence uint64
	var eventID, eventHash, actorIntent string
	var committed time.Time
	var receiptJSON []byte
	if err = tx.QueryRowContext(ctx, `SELECT sequence,event_id,event_hash,actor_intent_hash,event_time,receipt
		FROM events WHERE project_id=$1 ORDER BY sequence DESC LIMIT 1`, projectID).
		Scan(&sequence, &eventID, &eventHash, &actorIntent, &committed, &receiptJSON); err != nil {
		return LegacyImportStatus{}, unavailable(err)
	}
	var receipt controlplane.Receipt
	if err = json.Unmarshal(receiptJSON, &receipt); err != nil {
		return LegacyImportStatus{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE legacy_imports SET state='READY',projection_hash=$2,receipt=$3,updated_at=$4 WHERE project_id=$1`,
		projectID, actualProjectionHash, receiptJSON, e.now()); err != nil {
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

func (e *Engine) LegacyImportStatus(ctx context.Context, projectID string) (LegacyImportStatus, error) {
	var status LegacyImportStatus
	var receiptJSON []byte
	status.ProjectID = projectID
	err := e.db.QueryRowContext(ctx, `SELECT imported_sequence,expected_events,state,COALESCE(receipt,'{}'::jsonb)
		FROM legacy_imports WHERE project_id=$1`, projectID).
		Scan(&status.ImportedSequence, &status.ExpectedEvents, &status.State, &receiptJSON)
	if err != nil {
		return status, unavailable(err)
	}
	if string(receiptJSON) != "{}" {
		var receipt controlplane.Receipt
		if err = json.Unmarshal(receiptJSON, &receipt); err != nil {
			return status, err
		}
		status.Receipt = &receipt
	}
	return status, nil
}

func storedLegacyEvents(ctx context.Context, tx *sql.Tx, projectID string) ([]model.Event, error) {
	rows, err := tx.QueryContext(ctx, `SELECT legacy_bytes FROM events
		WHERE project_id=$1 AND legacy_bytes IS NOT NULL ORDER BY sequence`, projectID)
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	var events []model.Event
	for rows.Next() {
		var raw []byte
		var event model.Event
		if err = rows.Scan(&raw); err != nil {
			return nil, unavailable(err)
		}
		if err = json.Unmarshal(raw, &event); err != nil {
			return nil, &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "stored legacy event is invalid"}
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func verifyLegacyChain(events []model.Event) error {
	publicKeys := map[string]string{}
	previousHash := ""
	for index, event := range events {
		if event.Sequence != uint64(index+1) || event.PreviousHash != previousHash {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: fmt.Sprintf("legacy chain discontinuity at %s", event.ID)}
		}
		canonical := event
		canonical.Hash = ""
		canonical.Signature = ""
		raw, err := json.Marshal(canonical)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(raw)
		if hex.EncodeToString(hash[:]) != event.Hash {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: fmt.Sprintf("legacy hash mismatch at %s", event.ID)}
		}
		if event.Type == "agent.register" {
			payload, err := model.DecodePayload(event.Type, event.Data)
			if err != nil {
				return err
			}
			publicKeys[event.Actor] = payload.(*model.AgentRegistered).PublicKey
		}
		publicKey := publicKeys[event.Actor]
		if publicKey == "" || identity.Fingerprint(publicKey) != event.KeyFingerprint ||
			!identity.Verify(publicKey, event.Hash, event.Signature) {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: fmt.Sprintf("legacy signature mismatch at %s", event.ID)}
		}
		if event.Type == "agent.rotate-key" {
			payload, err := model.DecodePayload(event.Type, event.Data)
			if err != nil {
				return err
			}
			publicKeys[event.EntityID] = payload.(*model.AgentKeyRotated).PublicKey
		}
		previousHash = event.Hash
	}
	return nil
}

func projectionHash(state model.State) string {
	state.Integrity = model.Integrity{}
	raw, _ := json.Marshal(state)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
