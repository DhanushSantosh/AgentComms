package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/localcache"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/google/uuid"
)

const (
	maxLocalBodyBytes   = controlplane.MaxCommandBytes + 16*1024
	syncPageSize        = controlplane.MaxPageSize
	syncTimeout         = 10 * time.Second
	maxRuntimeListeners = 128
)

type Daemon struct {
	cache       *localcache.Cache
	remote      authorityClient
	syncMu      sync.Mutex
	syncing     map[string]*syncState
	dispatcher  *Dispatcher
	personal    bool
	shutdown    func()
	runtimeMode string
	projectID   string
	listeners   chan struct{}
}

type authorityClient interface {
	Command(context.Context, controlplane.Command) (controlplane.Event, controlplane.Receipt, error)
	Events(context.Context, string, controlplane.PageRequest) (controlplane.EventPage, error)
}

type syncState struct {
	done chan struct{}
	err  error
}

func New(cache *localcache.Cache, client authorityClient) (*Daemon, error) {
	if cache == nil || client == nil {
		return nil, errors.New("cache and authority client are required")
	}
	return &Daemon{
		cache: cache, remote: client, syncing: map[string]*syncState{},
		listeners: make(chan struct{}, maxRuntimeListeners),
	}, nil
}

func (d *Daemon) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "live", "runtime_mode": d.runtimeMode, "project_id": d.projectID,
			"protocol_version": controlplane.LocalDaemonProtocolVersion,
		})
	})
	mux.HandleFunc("POST /v1/admin/shutdown", d.shutdownDaemon)
	mux.HandleFunc("POST /v1/projects/{project}/commands", d.command)
	mux.HandleFunc("GET /v1/projects/{project}/state", d.state)
	mux.HandleFunc("GET /v1/projects/{project}/events", d.events)
	mux.HandleFunc("GET /v1/projects/{project}/invocations/next", d.nextInvocation)
	mux.HandleFunc("POST /v1/projects/{project}/sync", d.sync)
	mux.HandleFunc("POST /v1/projects/{project}/verify", d.verify)
	mux.HandleFunc("POST /v1/projects/{project}/drafts", d.saveDraft)
	mux.HandleFunc("GET /v1/projects/{project}/drafts", d.drafts)
	return mux
}

func (d *Daemon) shutdownDaemon(w http.ResponseWriter, _ *http.Request) {
	if d.shutdown == nil {
		writeControlError(w, &controlplane.Error{Code: controlplane.CodeUnavailable, Message: "daemon shutdown is unavailable"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"shutting_down": true})
	go d.shutdown()
}

func (d *Daemon) SetDispatcher(dispatcher *Dispatcher) {
	d.dispatcher = dispatcher
}

func (d *Daemon) SetPersonalMode(personal bool) {
	d.personal = personal
}

func (d *Daemon) SetShutdown(shutdown func()) {
	d.shutdown = shutdown
}

func (d *Daemon) SetIdentity(runtimeMode, projectID string) {
	d.runtimeMode = runtimeMode
	d.projectID = projectID
}

func (d *Daemon) metadata(sequence uint64, receipt *controlplane.Receipt) controlplane.ResultMetadata {
	if d.personal {
		return controlplane.ResultMetadata{
			Consistency: "PERSONAL_AUTHORITATIVE", ServerSequence: sequence,
			CacheSequence: sequence, Receipt: receipt, Connectivity: "LOCAL",
		}
	}
	return controlplane.ResultMetadata{
		Consistency: "AUTHORITATIVE", ServerSequence: sequence,
		CacheSequence: sequence, Receipt: receipt, Connectivity: "ONLINE",
	}
}

func (d *Daemon) verify(w http.ResponseWriter, r *http.Request) {
	var request struct {
		From uint64 `json:"from,omitempty"`
		To   uint64 `json:"to,omitempty"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := d.cache.VerifyRange(r.Context(), r.PathValue("project"), request.From, request.To); err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"verified": true, "from": request.From, "to": request.To})
}

func (d *Daemon) command(w http.ResponseWriter, r *http.Request) {
	var command controlplane.Command
	if !decodeJSON(w, r, &command) {
		return
	}
	if command.ProjectID != r.PathValue("project") {
		writeControlError(w, &controlplane.Error{Code: controlplane.CodeValidation, Message: "path and command project IDs differ"})
		return
	}
	event, receipt, err := d.remote.Command(r.Context(), command)
	if err != nil {
		writeControlError(w, err)
		return
	}
	if err = d.cache.Apply(r.Context(), event, receipt); err != nil {
		if syncErr := d.Sync(r.Context(), command.ProjectID); syncErr != nil {
			writeControlError(w, &controlplane.Error{
				Code: controlplane.CodeUnavailable,
				Message: "command committed authoritatively but local cache recovery failed: " +
					err.Error() + "; sync: " + syncErr.Error(),
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"event": event, "metadata": d.metadata(event.Sequence, &receipt),
	})
}

func (d *Daemon) state(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	ctx, cancel := context.WithTimeout(r.Context(), syncTimeout)
	defer cancel()
	syncErr := d.Sync(ctx, projectID)
	state, metadata, cacheErr := d.cache.State(r.Context(), projectID)
	if cacheErr != nil {
		if syncErr != nil {
			writeControlError(w, syncErr)
		} else {
			writeControlError(w, cacheErr)
		}
		return
	}
	if syncErr == nil {
		metadata = d.metadata(metadata.CacheSequence, nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": state, "metadata": metadata})
}

func (d *Daemon) events(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	page, err := localPageRequest(r)
	if err != nil {
		writeControlError(w, err)
		return
	}
	result, err := d.cache.Events(r.Context(), projectID, page)
	if err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (d *Daemon) nextInvocation(w http.ResponseWriter, r *http.Request) {
	actor := strings.TrimSpace(r.URL.Query().Get("actor"))
	runtimeID := strings.TrimSpace(r.URL.Query().Get("runtime"))
	if actor == "" {
		writeControlError(w, &controlplane.Error{Code: controlplane.CodeValidation, Message: "actor is required"})
		return
	}
	waitDuration := time.Duration(0)
	if raw := r.URL.Query().Get("wait_ms"); raw != "" {
		milliseconds, err := strconv.Atoi(raw)
		if err != nil || milliseconds < 0 || time.Duration(milliseconds)*time.Millisecond > controlplane.MaxInvocationListen {
			writeControlError(w, &controlplane.Error{
				Code:    controlplane.CodeValidation,
				Message: fmt.Sprintf("wait_ms must be from 0 to %d", controlplane.MaxInvocationListen.Milliseconds()),
			})
			return
		}
		waitDuration = time.Duration(milliseconds) * time.Millisecond
	}
	if waitDuration > 0 {
		select {
		case d.listeners <- struct{}{}:
			defer func() { <-d.listeners }()
		default:
			writeControlError(w, &controlplane.Error{
				Code: controlplane.CodeRateLimited, Message: "connected runtime listener capacity is exhausted",
				RetryAfter: time.Second,
			})
			return
		}
	}
	deadline := time.Now().Add(waitDuration)
	for {
		_ = d.Sync(r.Context(), r.PathValue("project"))
		state, metadata, err := d.cache.State(r.Context(), r.PathValue("project"))
		if err != nil {
			writeControlError(w, err)
			return
		}
		service.RefreshRuntimePresence(&state, time.Now().UTC())
		invocation, found := service.SelectNextInvocation(state, actor, runtimeID, time.Now().UTC())
		if found || waitDuration == 0 || !time.Now().Before(deadline) {
			writeJSON(w, http.StatusOK, map[string]any{
				"found": found, "invocation": invocation, "metadata": metadata,
			})
			return
		}
		select {
		case <-r.Context().Done():
			writeControlError(w, &controlplane.Error{Code: controlplane.CodeUnavailable, Message: r.Context().Err().Error()})
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (d *Daemon) sync(w http.ResponseWriter, r *http.Request) {
	if err := d.Sync(r.Context(), r.PathValue("project")); err != nil {
		writeControlError(w, err)
		return
	}
	_, metadata, err := d.cache.State(r.Context(), r.PathValue("project"))
	if err != nil {
		writeControlError(w, err)
		return
	}
	metadata = d.metadata(metadata.CacheSequence, nil)
	writeJSON(w, http.StatusOK, map[string]any{"synced": true, "metadata": metadata})
}

func (d *Daemon) saveDraft(w http.ResponseWriter, r *http.Request) {
	var draft controlplane.Draft
	if !decodeJSON(w, r, &draft) {
		return
	}
	if draft.ProjectID != r.PathValue("project") {
		writeControlError(w, &controlplane.Error{Code: controlplane.CodeValidation, Message: "path and draft project IDs differ"})
		return
	}
	if err := d.cache.SaveDraft(r.Context(), draft); err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"draft": draft, "authoritative": false})
}

func (d *Daemon) drafts(w http.ResponseWriter, r *http.Request) {
	limit := controlplane.DefaultPageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil {
			writeControlError(w, &controlplane.Error{Code: controlplane.CodeValidation, Message: "limit must be an integer"})
			return
		}
	}
	drafts, err := d.cache.Drafts(r.Context(), r.PathValue("project"), limit)
	if err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": drafts, "authoritative": false})
}

func (d *Daemon) Sync(ctx context.Context, projectID string) error {
	d.syncMu.Lock()
	if current, exists := d.syncing[projectID]; exists {
		d.syncMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-current.done:
			return current.err
		}
	}
	current := &syncState{done: make(chan struct{})}
	d.syncing[projectID] = current
	d.syncMu.Unlock()

	current.err = d.syncProject(ctx, projectID)
	close(current.done)
	d.syncMu.Lock()
	delete(d.syncing, projectID)
	d.syncMu.Unlock()
	return current.err
}

func (d *Daemon) syncProject(ctx context.Context, projectID string) error {
	sequence := uint64(0)
	if _, metadata, err := d.cache.State(ctx, projectID); err == nil {
		sequence = metadata.CacheSequence
	}
	cursor := controlplane.EncodeCursor(sequence)
	for {
		page, err := d.remote.Events(ctx, projectID, controlplane.PageRequest{Cursor: cursor, Limit: syncPageSize})
		if err != nil {
			return err
		}
		for _, record := range page.Items {
			if err = d.cache.Apply(ctx, record.Event, record.Receipt); err != nil {
				return err
			}
		}
		if page.NextCursor == "" {
			if d.dispatcher == nil {
				return nil
			}
			state, _, stateErr := d.cache.State(ctx, projectID)
			if stateErr != nil {
				return stateErr
			}
			service.RefreshRuntimePresence(&state, time.Now().UTC())
			return d.dispatcher.Dispatch(ctx, projectID, state)
		}
		cursor = page.NextCursor
	}
}

func (d *Daemon) submitConnectorCommand(ctx context.Context, projectID, actor, eventType, entityID string, payload any) error {
	raw, err := model.EncodePayload(eventType, payload)
	if err != nil {
		return err
	}
	credential, err := identity.ResolveCredential(identity.DefaultStore(), projectID, actor)
	if err != nil {
		return fmt.Errorf("resolve connector credential for %s: %w", actor, err)
	}
	command := controlplane.Command{
		ProjectID: projectID, Actor: actor, Type: eventType, EntityID: entityID,
		Payload: raw, IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
	}
	if err = command.Sign(credential.PrivateKey); err != nil {
		return err
	}
	event, receipt, err := d.remote.Command(ctx, command)
	if err != nil {
		return err
	}
	if err = d.cache.Apply(ctx, event, receipt); err != nil {
		return d.Sync(ctx, projectID)
	}
	return nil
}

func localPageRequest(r *http.Request) (controlplane.PageRequest, error) {
	page := controlplane.PageRequest{Cursor: r.URL.Query().Get("cursor")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return page, &controlplane.Error{Code: controlplane.CodeValidation, Message: "limit must be an integer"}
		}
		page.Limit = limit
	}
	_, err := page.BoundedLimit()
	return page, err
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxLocalBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeControlError(w, &controlplane.Error{Code: controlplane.CodeValidation, Message: "invalid request: " + err.Error()})
		return false
	}
	return true
}

func writeControlError(w http.ResponseWriter, err error) {
	var controlErr *controlplane.Error
	if !errors.As(err, &controlErr) {
		controlErr = &controlplane.Error{Code: controlplane.CodeUnavailable, Message: err.Error()}
	}
	status := http.StatusBadRequest
	switch controlErr.Code {
	case controlplane.CodeAuthorization:
		status = http.StatusForbidden
	case controlplane.CodeConflict, controlplane.CodeStalePrecondition:
		status = http.StatusConflict
	case controlplane.CodeRateLimited:
		status = http.StatusTooManyRequests
	case controlplane.CodeOffline, controlplane.CodeUnavailable:
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"code": controlErr.Code, "message": controlErr.Message,
		"retry_after_ms": controlErr.RetryAfter.Milliseconds(),
	}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func OfflineMutationError(commandType string) error {
	return &controlplane.Error{
		Code:    controlplane.CodeOffline,
		Message: fmt.Sprintf("%s requires the authoritative service; use an explicit local draft for offline preparation", commandType),
	}
}
