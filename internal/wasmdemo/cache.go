package wasmdemo

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/projection"
)

// cacheProject is one project's locally cached, verified event log plus its
// current projected state -- the in-memory equivalent of localcache.Cache's
// projects/events tables joined on project_id.
type cacheProject struct {
	sequence uint64
	head     string
	state    model.State
	events   []controlplane.Event
	receipts []controlplane.Receipt
}

// MemoryCache is an in-memory, in-process stand-in for
// internal/localcache.Cache. It implements internal/daemon's unexported
// cacheStore interface (VerifyRange, Apply, State, Events, SaveDraft,
// Drafts).
type MemoryCache struct {
	mu              sync.Mutex
	serverPublicKey string
	projects        map[string]*cacheProject
	drafts          map[string][]controlplane.Draft
	now             func() time.Time
}

// NewMemoryCache returns an empty MemoryCache. Unlike
// internal/localcache.Cache.Open, it does not require a server public key
// up front: in the WASM demo, MemoryCache and MemoryAuthority are wired
// together in the same process by the same caller (see
// cmd/agent-comms-tui-wasm), so there is no untrusted network boundary
// between "receipt was issued" and "receipt is being cached" the way there
// is for the real product's daemon talking to a remote authority server.
// Call SetServerPublicKey to opt into the real cryptographic receipt-
// signature check that internal/localcache.Cache always performs; without
// it, Apply/VerifyRange still perform every check that doesn't require a
// key (event/receipt field consistency, event content-hash verification,
// and hash-chain continuity) but skip receipt signature verification.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		projects: map[string]*cacheProject{},
		drafts:   map[string][]controlplane.Draft{},
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// SetServerPublicKey configures the key Apply and VerifyRange use to verify
// receipt signatures, exactly as internal/localcache.Cache.Open's
// serverPublicKey parameter does.
func (c *MemoryCache) SetServerPublicKey(publicKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.serverPublicKey = publicKey
}

// Apply verifies and appends event/receipt to projectID's cached log and
// recomputes its projected state, exactly reproducing
// internal/localcache.Cache.Apply's behavior (including real
// projection.ApplyEvent) against an in-memory map instead of SQLite.
func (c *MemoryCache) Apply(_ context.Context, event controlplane.Event, receipt controlplane.Receipt) error {
	if receipt.ProjectID != event.ProjectID || receipt.Sequence != event.Sequence ||
		receipt.EventID != event.ID || receipt.EventHash != event.Hash ||
		receipt.ActorIntentHash != event.ActorIntentHash {
		return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "event and receipt do not match"}
	}
	if !controlplane.VerifyEventHash(event) {
		return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "event hash is invalid"}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.serverPublicKey != "" && !controlplane.VerifyReceipt(receipt, c.serverPublicKey) {
		return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "server receipt signature is invalid"}
	}

	project, found := c.projects[event.ProjectID]
	if !found {
		project = &cacheProject{state: emptyState()}
		c.projects[event.ProjectID] = project
	}

	if event.Sequence <= project.sequence {
		existing := project.events[event.Sequence-1]
		if existing.Hash != event.Hash {
			return &controlplane.Error{Code: controlplane.CodeConflict, Message: "cache sequence contains a different event"}
		}
		return nil
	}
	if event.Sequence != project.sequence+1 || event.PreviousHash != project.head {
		return &controlplane.Error{Code: controlplane.CodeStalePrecondition, Message: "cache event stream has a gap"}
	}

	// Round-trip through a working copy, matching MemoryAuthority.Command's
	// (and the real cache's) transactional pattern: only commit on success.
	state := project.state
	modelEvent := model.Event{
		SchemaVersion: model.SchemaVersion, PayloadVersion: 1, ID: event.ID,
		Sequence: event.Sequence, Time: event.Time, Actor: event.Actor, Type: event.Type,
		EntityID: event.EntityID, Data: event.Payload, PreviousHash: event.PreviousHash, Hash: event.Hash,
	}
	if err := projection.ApplyEvent(&state, modelEvent); err != nil {
		return err
	}

	project.state = state
	project.sequence = event.Sequence
	project.head = event.Hash
	project.events = append(project.events, event)
	project.receipts = append(project.receipts, receipt)
	return nil
}

// State returns projectID's current cached, projected state, exactly as
// internal/localcache.Cache.State does.
func (c *MemoryCache) State(_ context.Context, projectID string) (model.State, controlplane.ResultMetadata, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	project, found := c.projects[projectID]
	if !found {
		return model.State{}, controlplane.ResultMetadata{}, &controlplane.Error{Code: controlplane.CodeOffline, Message: "project is not available in the local cache"}
	}
	state := project.state
	state.Integrity = model.Integrity{
		Verified: true, EventCount: int(project.sequence), Head: project.head, SyncState: "verified-cache",
	}
	return state, controlplane.ResultMetadata{
		Consistency: "CACHED", CacheSequence: project.sequence, Connectivity: "OFFLINE",
	}, nil
}

// Events returns a page of projectID's cached events, exactly as
// internal/localcache.Cache.Events does.
func (c *MemoryCache) Events(_ context.Context, projectID string, page controlplane.PageRequest) (controlplane.EventPage, error) {
	limit, err := page.BoundedLimit()
	if err != nil {
		return controlplane.EventPage{}, err
	}
	after, err := controlplane.DecodeCursor(page.Cursor)
	if err != nil {
		return controlplane.EventPage{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	project, found := c.projects[projectID]
	items := make([]controlplane.EventRecord, 0, limit)
	if found {
		for i, event := range project.events {
			if event.Sequence <= after {
				continue
			}
			items = append(items, controlplane.EventRecord{Event: event, Receipt: project.receipts[i]})
			if len(items) == limit+1 {
				break
			}
		}
	}
	nextCursor := ""
	if len(items) > limit {
		items = items[:limit]
		nextCursor = controlplane.EncodeCursor(items[len(items)-1].Event.Sequence)
	}
	sequence := after
	if len(items) > 0 {
		sequence = items[len(items)-1].Event.Sequence
	}
	return controlplane.EventPage{
		Items: items, NextCursor: nextCursor,
		Metadata: controlplane.ResultMetadata{
			Consistency: "CACHED", CacheSequence: sequence, Connectivity: "OFFLINE",
		},
	}, nil
}

// VerifyRange re-verifies the hash chain (and, if a server public key was
// configured, every receipt signature) across [from, to] for projectID,
// exactly as internal/localcache.Cache.VerifyRange does.
func (c *MemoryCache) VerifyRange(_ context.Context, projectID string, from, to uint64) error {
	if from == 0 {
		from = 1
	}
	if to != 0 && to < from {
		return &controlplane.Error{Code: controlplane.CodeValidation, Message: "invalid verification range"}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	project, found := c.projects[projectID]
	if !found {
		project = &cacheProject{}
	}

	previousHash := ""
	if from > 1 {
		if from-1 > uint64(len(project.events)) {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "verification range predecessor is missing"}
		}
		previousHash = project.events[from-2].Hash
	}
	expected := from
	upper := to
	if upper == 0 {
		upper = project.sequence
	}
	for _, event := range project.events {
		if event.Sequence < from {
			continue
		}
		if event.Sequence > upper {
			break
		}
		receipt := project.receipts[event.Sequence-1]
		if event.Sequence != expected || event.PreviousHash != previousHash {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: fmt.Sprintf("cache chain discontinuity at %s", event.ID)}
		}
		if !controlplane.VerifyEventHash(event) {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: fmt.Sprintf("cache hash mismatch at %s", event.ID)}
		}
		if c.serverPublicKey != "" && !controlplane.VerifyReceipt(receipt, c.serverPublicKey) {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: fmt.Sprintf("cache receipt mismatch at %s", event.ID)}
		}
		previousHash = event.Hash
		expected++
	}
	return nil
}

// SaveDraft stores draft, exactly reproducing
// internal/localcache.Cache.SaveDraft's validation and per-project quota
// checks against an in-memory map instead of SQLite.
func (c *MemoryCache) SaveDraft(_ context.Context, draft controlplane.Draft) error {
	if strings.TrimSpace(draft.ProjectID) == "" || strings.TrimSpace(draft.ID) == "" {
		return &controlplane.Error{Code: controlplane.CodeValidation, Message: "project and draft ID are required"}
	}
	switch draft.Kind {
	case "document", "message", "artifact":
	default:
		return &controlplane.Error{Code: controlplane.CodeValidation, Message: "draft kind must be document, message, or artifact"}
	}
	if len(draft.Body) == 0 || len(draft.Body) > controlplane.MaxDraftBytes {
		return &controlplane.Error{Code: controlplane.CodeValidation, Message: fmt.Sprintf("draft must contain 1 to %d bytes", controlplane.MaxDraftBytes)}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	existingDrafts := c.drafts[draft.ProjectID]
	existingIndex := -1
	var totalBytes int64
	for i, existing := range existingDrafts {
		totalBytes += int64(len(existing.Body))
		if existing.ID == draft.ID {
			existingIndex = i
		}
	}
	if existingIndex == -1 && len(existingDrafts) >= controlplane.MaxDraftsPerProject {
		return &controlplane.Error{Code: controlplane.CodeRateLimited, Message: "local draft count limit reached"}
	}
	var existingBytes int64
	if existingIndex != -1 {
		existingBytes = int64(len(existingDrafts[existingIndex].Body))
	}
	if totalBytes-existingBytes+int64(len(draft.Body)) > controlplane.MaxDraftStorageBytes {
		return &controlplane.Error{Code: controlplane.CodeRateLimited, Message: "local draft storage limit reached"}
	}

	now := c.now()
	if existingIndex != -1 {
		draft.CreatedAt = existingDrafts[existingIndex].CreatedAt
	} else if draft.CreatedAt.IsZero() {
		draft.CreatedAt = now
	}
	draft.UpdatedAt = now

	if existingIndex != -1 {
		existingDrafts[existingIndex] = draft
	} else {
		existingDrafts = append(existingDrafts, draft)
	}
	c.drafts[draft.ProjectID] = existingDrafts
	return nil
}

// Drafts returns projectID's drafts newest-updated-first, exactly as
// internal/localcache.Cache.Drafts does.
func (c *MemoryCache) Drafts(_ context.Context, projectID string, limit int) ([]controlplane.Draft, error) {
	if limit <= 0 {
		limit = controlplane.DefaultPageSize
	}
	if limit > controlplane.MaxPageSize {
		return nil, &controlplane.Error{Code: controlplane.CodeValidation, Message: "draft limit is too large"}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	source := c.drafts[projectID]
	sorted := make([]controlplane.Draft, len(source))
	copy(sorted, source)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].UpdatedAt.After(sorted[j].UpdatedAt) })
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted, nil
}
