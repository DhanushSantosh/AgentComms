// Package wasmdemo provides pure in-memory, dependency-free implementations
// of internal/daemon's authorityClient and cacheStore interfaces. They exist
// solely so the real product TUI (internal/tui) can run against a real
// controlplane.Command/Event/Receipt pipeline -- including real
// internal/protocol transition validation and real Ed25519 signing -- when
// compiled to GOOS=js GOARCH=wasm, where CGO (and therefore
// modernc.org/sqlite, used by internal/personalauthority and
// internal/localcache) is unavailable.
//
// MemoryAuthority (this file) mirrors internal/personalauthority.Engine's
// Command behavior against an in-memory map instead of *sql.DB.
// MemoryCache (cache.go) mirrors internal/localcache.Cache's Apply/State/
// Events/VerifyRange/SaveDraft/Drafts behavior the same way. Neither type
// duplicates the real validation or cryptography logic they wrap: Command
// still calls internal/protocol.ValidateTransition for every non-bootstrap
// transition, and receipts are still signed by a real *controlplane.Signer.
package wasmdemo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/projection"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
)

// authorityProject is one project's authoritative, hash-chained event log
// plus its current projected state -- the in-memory equivalent of a row in
// personalauthority's projects table joined with its events table.
type authorityProject struct {
	ownerID          string
	sequence         uint64
	headHash         string
	state            model.State
	events           []controlplane.Event
	receipts         []controlplane.Receipt
	byIdempotencyKey map[string]int // idempotency key -> index into events/receipts
}

// MemoryAuthority is an in-memory, in-process stand-in for
// internal/personalauthority.Engine. It implements internal/daemon's
// unexported authorityClient interface (Command, Events).
type MemoryAuthority struct {
	mu       sync.Mutex
	signer   *controlplane.Signer
	projects map[string]*authorityProject
	now      func() time.Time
}

// NewMemoryAuthority returns a MemoryAuthority that signs receipts with
// signer, exactly as internal/personalauthority.Engine does.
func NewMemoryAuthority(signer *controlplane.Signer) *MemoryAuthority {
	return &MemoryAuthority{
		signer:   signer,
		projects: map[string]*authorityProject{},
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// CreateProject registers a new project with an empty state and no events,
// mirroring personalauthority.Engine.CreateProject. It is not part of the
// authorityClient interface (real callers create projects out-of-band, via
// internal/runtimeinit, before the daemon ever starts) -- callers holding
// the concrete *MemoryAuthority (such as the WASM demo's seeding code) call
// this directly before issuing any Command for the project.
func (a *MemoryAuthority) CreateProject(_ context.Context, projectID, ownerID string) error {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(ownerID) == "" {
		return &controlplane.Error{Code: controlplane.CodeValidation, Message: "project and owner are required"}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.projects[projectID]; exists {
		return nil
	}
	a.projects[projectID] = &authorityProject{
		ownerID:          ownerID,
		state:            model.EmptyState(),
		byIdempotencyKey: map[string]int{},
	}
	return nil
}

// Command validates and applies command, exactly reproducing
// personalauthority.Engine.Mutate's behavior (including real
// internal/protocol.ValidateTransition validation and real
// controlplane.Signer signing) against in-memory state instead of SQLite.
func (a *MemoryAuthority) Command(_ context.Context, command controlplane.Command) (controlplane.Event, controlplane.Receipt, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	project, found := a.projects[command.ProjectID]
	if !found {
		return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeValidation, "project not found")
	}

	intentHash, err := command.IntentHash()
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeValidation, err.Error())
	}

	if idx, replayed := project.byIdempotencyKey[command.IdempotencyKey]; replayed {
		existingEvent := project.events[idx]
		if existingEvent.ActorIntentHash != intentHash {
			return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeConflict, "idempotency key was already used for a different command")
		}
		return existingEvent, project.receipts[idx], nil
	}

	now := a.now().Truncate(time.Microsecond)
	if err = command.Validate(now); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}

	// Round-trip project.state through JSON, exactly as the real engine
	// decodes state_json fresh from storage on every Mutate call. This
	// working copy is only committed back to project.state after every
	// following step succeeds, so a rejected command (like the real
	// engine's transaction rollback) never mutates project.state.
	stateJSON, err := json.Marshal(project.state)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, unavailable(err)
	}
	var state model.State
	if err = json.Unmarshal(stateJSON, &state); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, unavailable(err)
	}

	// Payload is decoded before signature verification because
	// commandPublicKey needs to inspect it: RequiresElevatedKey classifies
	// the most consequential identity/HUMAN-approval transitions as needing
	// the actor's elevated key, and that classification depends on the
	// decoded payload/target state. Mirrors personalauthority.Engine.Mutate.
	payload, err := model.DecodePayloadValue(command.Type, command.Payload)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeValidation, err.Error())
	}
	publicKey, err := commandPublicKey(state, command, payload, project.ownerID)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	if !command.Verify(publicKey) {
		return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeIntegrity, "actor command signature is invalid")
	}
	actorKeyFingerprint := identity.Fingerprint(publicKey)
	if command.ExpectedSequence != 0 && command.ExpectedSequence != project.sequence {
		return controlplane.Event{}, controlplane.Receipt{}, controlError(
			controlplane.CodeStalePrecondition,
			fmt.Sprintf("expected project sequence %d, current sequence is %d", command.ExpectedSequence, project.sequence),
		)
	}
	if command.Type == "agent.register" {
		registration, valid := payload.(model.AgentRegistered)
		if !valid || registration.PublicKey != command.PublicKey {
			return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeIntegrity, "registration payload and command public keys differ")
		}
	}
	if command.Type == "agent.activate" && project.sequence == 1 && command.Actor == project.ownerID && command.EntityID == project.ownerID {
		activation, valid := payload.(model.AgentActivated)
		if !valid || activation.Role != model.RoleOwner {
			return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeAuthorization, "initial owner activation must assign OWNER")
		}
	} else {
		// This is the one call this in-memory authority must never skip or
		// stub out: real internal/protocol transition validation, unchanged
		// from what personalauthority.Engine.Mutate calls.
		payload, err = protocol.ValidateTransition(state, command.Actor, command.Type, command.EntityID, payload, now)
		if err != nil {
			return controlplane.Event{}, controlplane.Receipt{}, controlplane.ClassifyValidationError(err)
		}
	}
	normalizedPayload, err := model.EncodePayload(command.Type, payload)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeValidation, err.Error())
	}

	event := controlplane.Event{
		ProjectID: command.ProjectID, Sequence: project.sequence + 1,
		ID: fmt.Sprintf("evt-%020d", project.sequence+1), Time: now, Actor: command.Actor,
		ActorKeyFingerprint: actorKeyFingerprint,
		Type:                command.Type, EntityID: command.EntityID, Payload: normalizedPayload,
		PreviousHash: project.headHash, ActorIntentHash: intentHash, IdempotencyKey: command.IdempotencyKey,
	}
	event.Hash, err = controlplane.HashEvent(event)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	receipt := controlplane.Receipt{
		ProjectID: event.ProjectID, Sequence: event.Sequence, EventID: event.ID,
		EventHash: event.Hash, ActorIntentHash: intentHash, CommittedAt: now,
	}
	if err = a.signer.SignReceipt(&receipt); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	if err = projection.ApplyEvent(&state, model.Event{
		SchemaVersion: model.SchemaVersion, PayloadVersion: 1, ID: event.ID,
		Sequence: event.Sequence, Time: event.Time, Actor: event.Actor, Type: event.Type,
		EntityID: event.EntityID, Data: event.Payload, PreviousHash: event.PreviousHash, Hash: event.Hash,
	}); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}

	// Commit: everything above succeeded, so this is the only point where
	// project state actually advances -- mirrors the real engine's tx.Commit.
	project.state = state
	project.sequence = event.Sequence
	project.headHash = event.Hash
	project.events = append(project.events, event)
	project.receipts = append(project.receipts, receipt)
	project.byIdempotencyKey[command.IdempotencyKey] = len(project.events) - 1

	return event, receipt, nil
}

// Events returns a page of project's committed events, newest-last, exactly
// as personalauthority.Engine.Events does.
func (a *MemoryAuthority) Events(_ context.Context, projectID string, page controlplane.PageRequest) (controlplane.EventPage, error) {
	limit, err := page.BoundedLimit()
	if err != nil {
		return controlplane.EventPage{}, err
	}
	after, err := controlplane.DecodeCursor(page.Cursor)
	if err != nil {
		return controlplane.EventPage{}, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	project, found := a.projects[projectID]
	if !found {
		return controlplane.EventPage{}, controlError(controlplane.CodeValidation, "project not found")
	}

	items := make([]controlplane.EventRecord, 0, limit)
	for i, event := range project.events {
		if event.Sequence <= after {
			continue
		}
		items = append(items, controlplane.EventRecord{Event: event, Receipt: project.receipts[i]})
		if len(items) == limit+1 {
			break
		}
	}
	nextCursor := ""
	if len(items) > limit {
		items = items[:limit]
		nextCursor = controlplane.EncodeCursor(items[len(items)-1].Event.Sequence)
	}
	last := after
	if len(items) > 0 {
		last = items[len(items)-1].Event.Sequence
	}
	return controlplane.EventPage{
		Items: items, NextCursor: nextCursor,
		Metadata: controlplane.ResultMetadata{
			Consistency: "PERSONAL_AUTHORITATIVE", ServerSequence: last,
			CacheSequence: last, Connectivity: "LOCAL",
		},
	}, nil
}

// commandPublicKey resolves the public key command's signature must verify
// against, exactly as personalauthority.Engine's unexported function of the
// same name does (ownerID replaces that function's implicit reliance on a
// projects-table row -- here it is threaded in explicitly).
func commandPublicKey(state model.State, command controlplane.Command, payload any, ownerID string) (string, error) {
	if command.Type == "agent.register" {
		if command.Actor != command.EntityID || command.PublicKey == "" {
			return "", controlError(controlplane.CodeAuthorization, "registration must be self-signed with a public key")
		}
		return command.PublicKey, nil
	}
	agent, found := state.Agents[command.Actor]
	if !found {
		return "", controlError(controlplane.CodeAuthorization, "actor is not registered")
	}
	if agent.ElevatedPublicKey != "" && protocol.RequiresElevatedKey(state, command.Actor, command.Type, command.EntityID, payload) {
		return agent.ElevatedPublicKey, nil
	}
	return agent.PublicKey, nil
}

func controlError(code controlplane.ErrorCode, message string) error {
	return &controlplane.Error{Code: code, Message: message}
}

func unavailable(err error) error {
	if controlErr, ok := err.(*controlplane.Error); ok {
		return controlErr
	}
	return controlError(controlplane.CodeUnavailable, err.Error())
}
