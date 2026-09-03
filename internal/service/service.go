package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/daemonclient"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
	"github.com/DhanushSantosh/AgentComms/internal/remote"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/google/uuid"
)

type Service struct {
	Store         *store.Store
	remote        *daemonclient.Client
	remoteErr     error
	recoverRemote func() error
	// PassphrasePrompt supplies the passphrase to decrypt an actor's
	// elevated credential (identity.ElevatedActor) when a transition
	// requires it (protocol.RequiresElevatedKey) and one is registered. nil
	// (the default) means no interactive prompt is wired up; those
	// transitions then fail with a clear error rather than silently
	// falling back to the actor's unprotected primary key.
	PassphrasePrompt func(actor string) (string, error)
	// AmbiguousActor is set once, by whichever caller resolved the actor
	// this Service will sign as (internal/app's PersistentPreRunE, shared
	// by the CLI, TUI, and MCP), when that resolution came from the
	// legacy, machine-wide identity.UserConfig.ActiveProfile fallback (no
	// recognized provider session ID at all) *and* the project has two or
	// more locally-registered identities to silently choose between.
	// ExecuteWithPassphrase refuses outright rather than sign under a
	// value this ambiguous -- confirmed live as a real defect, not
	// theoretical: exactly this condition let one agent's governed writes
	// get silently signed under a different, unrelated agent's identity.
	// See RFC 0017. Default false (every caller that never sets it, e.g.
	// internal/worker's standalone worker-loop Service instances, is
	// completely unaffected).
	AmbiguousActor bool
}

func New(root string) *Service {
	instance := &Service{Store: store.Open(root)}
	cfg, err := instance.Store.ConfigStrict()
	return configure(instance, cfg, err)
}

func NewTolerant(root string) *Service {
	instance := &Service{Store: store.Open(root)}
	cfg, err := instance.Store.Config()
	return configure(instance, cfg, err)
}

func configure(instance *Service, cfg store.Config, err error) *Service {
	if err != nil {
		instance.remoteErr = err
		return instance
	}
	if cfg.RuntimeMode != "service" && cfg.RuntimeMode != "personal" {
		instance.remoteErr = errors.New("unsupported runtime mode; initialize a personal or service project")
		return instance
	}
	if cfg.DaemonEndpoint == "" {
		instance.remoteErr = errors.New("service runtime is missing daemon endpoint")
		return instance
	}
	instance.remote, instance.remoteErr = daemonclient.New(cfg.DaemonEndpoint, controlplane.DefaultRequestTimeout)
	return instance
}

// NewWithRemote constructs a Service around an already-open Store and an
// already-built daemon client, bypassing the on-disk config read and
// real-socket dial that New/NewTolerant perform via configure. Its only real
// caller is the WASM demo entrypoint, which builds both projectStore and
// remote itself against an in-process daemon -- no real filesystem or
// network involved -- so there is no store.Config to read here and nothing
// for configure to validate. remoteErr is left nil (remote is assumed
// already valid), and recoverRemote/PassphrasePrompt/AmbiguousActor are left
// at their zero values exactly as New/NewTolerant leave them on their own
// success path -- those are populated later by whichever caller needs them
// (SetRemoteRecovery, internal/app's PersistentPreRunE), never by the
// constructor itself. Every other real caller keeps using New/NewTolerant
// unchanged.
func NewWithRemote(projectStore *store.Store, remote *daemonclient.Client) *Service {
	return &Service{Store: projectStore, remote: remote}
}

func (s *Service) SetRemoteRecovery(recoverRemote func() error) {
	s.recoverRemote = recoverRemote
}
func (s *Service) State() (model.State, error) {
	if s.remoteErr != nil {
		return model.State{}, s.remoteErr
	}
	cfg, err := s.Store.Config()
	if err != nil {
		return model.State{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlplane.DefaultRequestTimeout)
	defer cancel()
	var state model.State
	var metadata controlplane.ResultMetadata
	if err := s.retryOnDaemonOffline(ctx, func() error {
		var stateErr error
		state, metadata, stateErr = s.remote.State(ctx, cfg.ProjectID)
		return stateErr
	}); err != nil {
		return state, err
	}
	state.Integrity.Consistency = metadata.Consistency
	state.Integrity.ServerSequence = metadata.ServerSequence
	state.Integrity.CacheSequence = metadata.CacheSequence
	state.Integrity.Connectivity = metadata.Connectivity
	protocol.RefreshRuntimePresence(&state, time.Now().UTC())
	return state, nil
}

// RequestApprovalForOperation creates an approval whose digest is derived
// from the exact operation clients will later submit. Callers never handle
// approval digests directly; invocation defaults are normalized against the
// same current state used by protocol validation.
func (s *Service) RequestApprovalForOperation(actor, approvalID, tier, typ, id string, payload any, reason string, ttl time.Duration) (model.Event, error) {
	if ttl <= 0 {
		return model.Event{}, errors.New("approval TTL must be positive")
	}
	tier = strings.ToUpper(strings.TrimSpace(tier))
	action := ""
	affected := []string{}
	subject := payload
	switch typ {
	case "message.post":
		message, ok := payload.(model.MessagePosted)
		if !ok || message.Kind != "CONTRACT" {
			return model.Event{}, errors.New("payload-bound message approval requires a CONTRACT message")
		}
		action = "contract:" + id
		affected = append(affected, message.To...)
	case "invocation.request":
		invocation, ok := payload.(model.InvocationRequested)
		if !ok {
			return model.Event{}, errors.New("payload-bound invocation approval requires an invocation request")
		}
		state, err := s.State()
		if err != nil {
			return model.Event{}, err
		}
		invocation = protocol.NormalizeInvocationApprovalSubject(state, invocation)
		subject = invocation
		affected = []string{invocation.Target}
		if tier == "HUMAN" {
			action = "invocation-sensitive:" + id
		} else {
			action = "invocation:" + id
		}
	default:
		return model.Event{}, errors.New("operation does not support payload-bound approval")
	}
	expires := time.Now().UTC().Add(ttl)
	subjectJSON := protocol.ApprovalSubjectJSON(actor, typ, id, subject)
	return s.Execute(actor, "approval.request", approvalID, model.ApprovalRequested{
		Tier: tier, Action: action, SubjectDigest: protocol.ApprovalSubjectDigestFromJSON(subjectJSON), Subject: subjectJSON,
		Reason: reason, Affected: affected, ExpiresAt: &expires,
	})
}

// retryOnDaemonOffline runs op up to 3 times with the same backoff-and-
// recover shape signed writes have always had (originally only in
// executeRemoteWithCredential, now shared): on a retryable daemon error
// (CodeOffline/CodeUnavailable/CodeRateLimited) it calls s.recoverRemote
// (wired to ensureDaemon, internal/app/app.go) once per attempt before
// backing off and trying again. A non-retryable error, or an error
// recoverRemote itself can't fix, returns immediately.
//
// Extracted so plain reads (State) get the same resilience signed writes
// already had -- without it, a single transient "local daemon is
// unavailable" blip during `runtime interactive-serve`'s periodic
// heartbeat (internal/app/app.go's heartbeatInteractiveRuntime, which calls
// State() before Execute) tore down the entire live interactive session on
// the very next tick, with nothing ever getting a chance to reconnect.
func (s *Service) retryOnDaemonOffline(ctx context.Context, op func() error) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = op()
		if err == nil {
			return nil
		}
		var controlErr *controlplane.Error
		if !errors.As(err, &controlErr) ||
			(controlErr.Code != controlplane.CodeOffline && controlErr.Code != controlplane.CodeUnavailable &&
				controlErr.Code != controlplane.CodeRateLimited) {
			return err
		}
		if s.recoverRemote != nil &&
			(controlErr.Code == controlplane.CodeOffline || controlErr.Code == controlplane.CodeUnavailable) {
			if recoveryErr := s.recoverRemote(); recoveryErr != nil {
				return recoveryErr
			}
		}
		delay := time.Duration(50*(1<<attempt)) * time.Millisecond
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
	return err
}

func (s *Service) Verify(from, to uint64) error {
	if s.remoteErr != nil {
		return s.remoteErr
	}
	cfg, err := s.Store.Config()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlplane.DefaultRequestTimeout)
	defer cancel()
	return s.remote.Verify(ctx, cfg.ProjectID, from, to)
}

func (s *Service) NextInvocation(actor, runtimeID string) (model.Invocation, bool, error) {
	return s.ListenInvocation(actor, runtimeID, 0)
}

func (s *Service) ListenInvocation(actor, runtimeID string, wait time.Duration) (model.Invocation, bool, error) {
	if wait < 0 || wait > controlplane.MaxInvocationListen {
		return model.Invocation{}, false,
			fmt.Errorf("invocation listen duration must be from 0 to %s", controlplane.MaxInvocationListen)
	}
	if s.remoteErr != nil {
		return model.Invocation{}, false, s.remoteErr
	}
	if s.remote != nil {
		cfg, err := s.Store.Config()
		if err != nil {
			return model.Invocation{}, false, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), wait+controlplane.DefaultRequestTimeout)
		defer cancel()
		invocation, found, _, err := s.remote.NextInvocation(ctx, cfg.ProjectID, actor, runtimeID, wait)
		return invocation, found, err
	}
	deadline := time.Now().Add(wait)
	for {
		state, err := s.State()
		if err != nil {
			return model.Invocation{}, false, err
		}
		invocation, found := SelectNextInvocation(state, actor, runtimeID, time.Now().UTC())
		if found || wait == 0 || !time.Now().Before(deadline) {
			return invocation, found, nil
		}
		time.Sleep(min(250*time.Millisecond, time.Until(deadline)))
	}
}

func SelectNextInvocation(state model.State, actor, runtimeID string, now time.Time) (model.Invocation, bool) {
	var selectedRuntime model.AgentRuntime
	if runtimeID != "" {
		runtime, exists := state.AgentRuntimes[runtimeID]
		if !exists || runtime.AgentID != actor || runtime.Status != "ONLINE" ||
			runtimeLoad(state, runtimeID) >= runtime.MaxConcurrent {
			return model.Invocation{}, false
		}
		selectedRuntime = runtime
	}
	var selected model.Invocation
	found := false
	priorityRank := map[string]int{"URGENT": 4, "HIGH": 3, "NORMAL": 2, "LOW": 1}
	for _, invocation := range state.Invocations {
		if invocation.Target != actor || (invocation.Status != "PENDING" && invocation.Status != "NOTIFIED") {
			continue
		}
		consumerMode := invocation.ConsumerMode
		if consumerMode == "" {
			consumerMode = model.ConsumerModeEither
		}
		if runtimeID == "" {
			if consumerMode != model.ConsumerModeEither || invocation.PreferredRuntimeID != "" {
				continue
			}
		} else {
			kind := selectedRuntime.Kind
			if kind == "" {
				kind = model.RuntimeKindWorker
			}
			if (consumerMode == model.ConsumerModeInteractiveOnly && kind != model.RuntimeKindInteractive) ||
				(consumerMode == model.ConsumerModeWorkerOnly && kind != model.RuntimeKindWorker) ||
				(invocation.PreferredRuntimeID != "" && invocation.PreferredRuntimeID != runtimeID) {
				continue
			}
		}
		if invocation.Deadline != nil && !invocation.Deadline.After(now) {
			continue
		}
		if !found || priorityRank[invocation.Priority] > priorityRank[selected.Priority] ||
			(priorityRank[invocation.Priority] == priorityRank[selected.Priority] &&
				invocation.CreatedAt.Before(selected.CreatedAt)) {
			selected = invocation
			found = true
		}
	}
	return selected, found
}

func runtimeLoad(state model.State, runtimeID string) int {
	load := 0
	for _, invocation := range state.Invocations {
		if invocation.RuntimeID == runtimeID &&
			(invocation.Status == "CLAIMED" || invocation.Status == "RUNNING" ||
				invocation.Status == "WAITING") {
			load++
		}
	}
	return load
}

type InvocationDeliveryResult struct {
	Outcome    string                   `json:"outcome"`
	Invocation string                   `json:"invocation_id"`
	DeliveryID string                   `json:"delivery_id,omitempty"`
	RuntimeID  string                   `json:"runtime_id,omitempty"`
	Transport  string                   `json:"transport,omitempty"`
	Evidence   []model.DeliveryEvidence `json:"evidence,omitempty"`
	Error      string                   `json:"error,omitempty"`
}

func SummarizeInvocationDelivery(
	state model.State,
	invocationID string,
	deliveryID string,
	localHostID string,
) (InvocationDeliveryResult, bool) {
	invocation, exists := state.Invocations[invocationID]
	if !exists {
		return InvocationDeliveryResult{}, false
	}
	var selected model.InvocationDelivery
	found := false
	for _, delivery := range state.InvocationDeliveries {
		if delivery.InvocationID != invocationID ||
			(deliveryID != "" && delivery.ID != deliveryID) {
			continue
		}
		if !found || delivery.Attempt > selected.Attempt {
			selected = delivery
			found = true
		}
	}
	result := InvocationDeliveryResult{Invocation: invocationID}
	if !found {
		mode := invocation.ConsumerMode
		if mode == "" {
			mode = model.ConsumerModeEither
		}
		if mode != model.ConsumerModeInteractiveOnly {
			result.Outcome = "PENDING_CONSUMER"
			return result, true
		}
		eligibleInteractive := 0
		for _, runtimeState := range state.AgentRuntimes {
			if runtimeState.AgentID == invocation.Target &&
				runtimeState.Kind == model.RuntimeKindInteractive &&
				runtimeState.Connector == "INTERACTIVE" &&
				runtimeState.Status == "ONLINE" &&
				localHostID != "" && runtimeState.HostID == localHostID {
				eligibleInteractive++
			}
		}
		if invocation.PreferredRuntimeID == "" && eligibleInteractive > 1 {
			result.Outcome = "AMBIGUOUS"
			result.Error = "multiple local interactive runtimes are eligible; select a preferred runtime"
		} else {
			result.Outcome = "UNAVAILABLE"
			result.Error = "no compatible local interactive delivery completed"
		}
		return result, true
	}
	result.DeliveryID = selected.ID
	result.RuntimeID = selected.RuntimeID
	result.Transport = selected.Transport
	result.Evidence = selected.Evidence
	result.Error = selected.Error
	switch selected.Status {
	case "SUCCEEDED":
		result.Outcome = "SUCCEEDED"
	case "NOTIFIED":
		result.Outcome = "LEGACY_UNVERIFIED"
		if result.Error == "" {
			result.Error = "legacy notification has no transport evidence"
		}
	case "ATTEMPTED":
		result.Outcome = "ATTEMPTED"
	default:
		result.Outcome = "UNAVAILABLE"
	}
	return result, true
}

func (s *Service) History(page controlplane.PageRequest) (controlplane.EventPage, error) {
	if s.remoteErr != nil {
		return controlplane.EventPage{}, s.remoteErr
	}
	cfg, err := s.Store.Config()
	if err != nil {
		return controlplane.EventPage{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlplane.DefaultRequestTimeout)
	defer cancel()
	return s.remote.Events(ctx, cfg.ProjectID, page)
}

func (s *Service) Sync() (controlplane.ResultMetadata, error) {
	if s.remoteErr != nil {
		return controlplane.ResultMetadata{}, s.remoteErr
	}
	if s.remote == nil {
		return controlplane.ResultMetadata{}, errors.New("authoritative service mode is not active")
	}
	cfg, err := s.Store.Config()
	if err != nil {
		return controlplane.ResultMetadata{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlplane.DefaultRequestTimeout)
	defer cancel()
	return s.remote.Sync(ctx, cfg.ProjectID)
}

func (s *Service) SaveDraft(id, kind string, body json.RawMessage) error {
	if s.remoteErr != nil {
		return s.remoteErr
	}
	if s.remote == nil {
		return errors.New("drafts require authoritative service mode")
	}
	cfg, err := s.Store.Config()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlplane.DefaultRequestTimeout)
	defer cancel()
	return s.remote.SaveDraft(ctx, controlplane.Draft{ProjectID: cfg.ProjectID, ID: id, Kind: kind, Body: body})
}

func (s *Service) Drafts(limit int) ([]controlplane.Draft, error) {
	if s.remoteErr != nil {
		return nil, s.remoteErr
	}
	if s.remote == nil {
		return nil, errors.New("drafts require authoritative service mode")
	}
	cfg, err := s.Store.Config()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlplane.DefaultRequestTimeout)
	defer cancel()
	return s.remote.Drafts(ctx, cfg.ProjectID, limit)
}

func ValidateTransition(st model.State, actor, typ, id string, payload any, now time.Time) (any, error) {
	return protocol.ValidateTransition(st, actor, typ, id, payload, now)
}

func RefreshRuntimePresence(state *model.State, now time.Time) {
	protocol.RefreshRuntimePresence(state, now)
}

func (s *Service) Execute(actor, typ, id string, payload any) (model.Event, error) {
	return s.ExecuteWithPassphrase(actor, typ, id, payload, "")
}

// ExecuteWithPassphrase behaves exactly like Execute, except that when the
// transition actually requires the actor's elevated key
// (protocol.RequiresElevatedKey) and passphrase is non-empty, that value
// decrypts it directly instead of calling PassphrasePrompt. This lets a
// caller that already collected the passphrase through its own UI -- the
// TUI's masked form field, rather than a separate terminal prompt racing
// bubbletea's raw-mode stdin -- supply it inline. An empty passphrase falls
// back to the existing PassphrasePrompt behavior unchanged, so every
// existing caller (CLI, MCP) is unaffected.
func (s *Service) ExecuteWithPassphrase(actor, typ, id string, payload any, passphrase string) (model.Event, error) {
	if s.AmbiguousActor {
		return model.Event{}, fmt.Errorf(
			"refusing to sign %s as %q: this actor was resolved from the shared, machine-wide"+
				" active-profile default, and this project has more than one locally-registered"+
				" identity to choose between -- pass --actor explicitly, or set AGENT_COMMS_ACTOR"+
				" (both required for any invocation with no recognized provider session, e.g. an"+
				" opencode-based agent or a background script); run `agent-comms profile current`"+
				" to see exactly how this was resolved", typ, actor,
		)
	}
	if s.remoteErr != nil {
		return model.Event{}, s.remoteErr
	}
	return s.executeRemote(actor, typ, id, payload, passphrase)
}

func (s *Service) executeRemote(actor, typ, id string, payload any, passphrase string) (model.Event, error) {
	cfg, err := s.Store.Config()
	if err != nil {
		return model.Event{}, err
	}
	credential, err := identity.ResolveCredential(s.Store.Credentials, cfg.ProjectID, actor)
	if err != nil {
		return model.Event{}, fmt.Errorf("credential for %s: %w", actor, err)
	}
	credential, err = s.elevateCredentialIfNeeded(cfg, actor, typ, id, payload, credential, passphrase)
	if err != nil {
		return model.Event{}, err
	}
	return s.executeRemoteWithCredential(cfg, actor, typ, id, payload, credential)
}

// elevateCredentialIfNeeded swaps in actor's elevated credential when
// protocol.RequiresElevatedKey classifies typ/id/payload as needing it and
// one is registered for actor -- the identical classification server-side
// verification applies (internal/personalauthority, internal/authority), so
// client and server never disagree about which key a transition needed.
// Falls back to primary, unchanged, whenever no elevated key is registered:
// backward compatible with every existing single-key setup. A read failure
// resolving the elevated credential also falls back to primary rather than
// hard-failing here -- if the server actually requires the elevated key
// (agent.ElevatedPublicKey set on the agent record), a primary-signed
// command still gets rejected there with an integrity error, never silently
// accepted with weaker protection than the record calls for.
func (s *Service) elevateCredentialIfNeeded(
	cfg store.Config, actor, typ, id string, payload any, primary identity.Credential, overridePassphrase string,
) (identity.Credential, error) {
	if typ != "agent.activate" && typ != "agent.switch-role" && typ != "approval.approve" && typ != "agent.revoke" && typ != "agent.delete" {
		return primary, nil
	}
	// st stays the zero-value model.State{} for agent.activate and
	// agent.switch-role, whose RequiresElevatedKey branches read only the
	// payload, never state -- see the invariant documented on
	// RequiresElevatedKey itself (and mirrored
	// by internal/authority/postgres.go's scopedElevationState, which makes
	// the same per-type assumption via targeted SQL instead of a full
	// fetch). Only fetch state for the transition types that actually need it.
	var st model.State
	if typ == "approval.approve" || typ == "agent.revoke" || typ == "agent.delete" {
		fetched, err := s.State()
		if err != nil {
			return identity.Credential{}, err
		}
		st = fetched
	}
	if !protocol.RequiresElevatedKey(st, actor, typ, id, payload) {
		return primary, nil
	}
	elevated, err := s.Store.Credentials.Get(cfg.ProjectID, identity.ElevatedActor(actor))
	if err != nil {
		return primary, nil
	}
	return s.decryptElevatedKey(elevated, actor, fmt.Sprintf("%s for %s", typ, actor), overridePassphrase)
}

// decryptElevatedKey decrypts an already-resolved elevated credential --
// shared by elevateCredentialIfNeeded above (which falls back to the
// actor's primary key when no elevated key is registered at all) and
// DeleteProject below (which requires one to already be registered -- see
// RFC 0020), so there is exactly one implementation of "decrypt with
// whatever passphrase source is available," not two that could drift.
// context names the caller's own action for the "no passphrase prompt
// available" error (e.g. "agent.activate for builder", "deleting project
// acme"). overridePassphrase, when non-empty, decrypts directly instead of
// calling s.PassphrasePrompt -- see ExecuteWithPassphrase's own doc for why
// (a caller that already collected it through its own UI, e.g. the TUI's
// masked form field, rather than a separate terminal prompt racing
// bubbletea's raw-mode stdin).
func (s *Service) decryptElevatedKey(elevated identity.Credential, actor, context, overridePassphrase string) (identity.Credential, error) {
	if !elevated.Encrypted {
		return elevated, nil
	}
	if overridePassphrase != "" {
		return elevated.Decrypted(overridePassphrase)
	}
	if s.PassphrasePrompt == nil {
		return identity.Credential{}, fmt.Errorf("%s requires the elevated key, but no passphrase prompt is available in this context", context)
	}
	passphrase, err := s.PassphrasePrompt(actor)
	if err != nil {
		return identity.Credential{}, err
	}
	return elevated.Decrypted(passphrase)
}

func (s *Service) executeRemoteWithCredential(
	cfg store.Config,
	actor, typ, id string,
	payload any,
	credential identity.Credential,
) (model.Event, error) {
	raw, err := model.EncodePayload(typ, payload)
	if err != nil {
		return model.Event{}, err
	}
	command := controlplane.Command{
		ProjectID: cfg.ProjectID, Actor: actor, Type: typ, EntityID: id,
		Payload: raw, IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
	}
	if typ == "agent.register" {
		command.PublicKey = credential.PublicKey
	}
	if err = command.Sign(credential.PrivateKey); err != nil {
		return model.Event{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlplane.DefaultRequestTimeout)
	defer cancel()
	var event controlplane.Event
	var metadata controlplane.ResultMetadata
	if err := s.retryOnDaemonOffline(ctx, func() error {
		var commandErr error
		event, metadata, commandErr = s.remote.Command(ctx, command)
		return commandErr
	}); err != nil {
		return model.Event{}, err
	}
	return model.Event{
		SchemaVersion: model.SchemaVersion, PayloadVersion: 1, ID: event.ID,
		Sequence: event.Sequence, Time: event.Time, Actor: event.Actor, Type: event.Type,
		EntityID: event.EntityID, Data: event.Payload, PreviousHash: event.PreviousHash,
		Hash: event.Hash, Signature: command.Signature,
		KeyFingerprint: event.ActorKeyFingerprint,
		ServerReceipt:  metadata.Receipt, Consistency: metadata.Consistency,
		Connectivity: metadata.Connectivity,
	}, nil
}

// Register generates the principal credential and records the registration
// through the configured authoritative daemon.
func (s *Service) Register(actor, display string, pt model.PrincipalType) (model.Event, error) {
	cfg, e := s.Store.Config()
	if e != nil {
		return model.Event{}, e
	}
	cred, e := identity.Generate(cfg.ProjectID, actor)
	if e != nil {
		return model.Event{}, e
	}
	payload := model.AgentRegistered{PublicKey: cred.PublicKey, PrincipalType: pt, DisplayName: display}

	st, stateErr := s.State()
	if stateErr != nil {
		return model.Event{}, stateErr
	}
	if _, exists := st.Agents[actor]; exists {
		return model.Event{}, errors.New("principal already exists")
	}
	event, e := s.executeRemoteWithCredential(cfg, actor, "agent.register", actor, payload, cred)
	if e != nil {
		return model.Event{}, e
	}

	if e = s.Store.Credentials.Put(cred); e != nil {
		return event, fmt.Errorf("registration recorded but credential could not be stored: %w", e)
	}
	user, e := identity.LoadUserConfig()
	if e != nil {
		return event, fmt.Errorf("registration recorded but could not load the identity profile registry: %w", e)
	}
	name := cfg.ProjectID + ":" + actor
	user.Profiles[name] = identity.Profile{Name: name, ProjectID: cfg.ProjectID, Actor: actor, ProjectRoot: s.Store.Root, HostLabel: os.Getenv("AGENT_COMMS_HOST_LABEL")}
	// Scoped to the registering session when one is recognized, rather
	// than the legacy machine-wide field every other process on this
	// account would otherwise inherit too -- see RFC 0016.
	//
	// identity.DetectProviderSessionID, not sessionbind.Capture: this
	// package cannot import internal/sessionbind without a real cycle
	// (sessionbind -> worker -> service), so it uses the narrower,
	// dependency-free subset of the same detection logic instead -- see
	// DetectProviderSessionID's own doc comment.
	sessionID := identity.DetectProviderSessionID()
	// Only the convenience of defaulting a *real, isolated* session to its
	// own just-registered identity -- never the shared, machine-wide
	// legacy field. That field has no scoping at all, so a session-less
	// caller's (an opencode-based agent, a script) own convenience default
	// would otherwise silently become every other session-less caller's
	// default too, including a human's own plain terminal -- confirmed
	// live: exactly this is how a project's shared legacy slot ended up
	// permanently pointed at an agent instead of its human owner, with no
	// further action from anyone. A session-less caller must already pass
	// --actor/AGENT_COMMS_ACTOR explicitly on every write regardless (see
	// RFC 0017), so it never needed this convenience to begin with.
	if sessionID != "" && user.ActiveProfileFor(sessionID) == "" {
		user.SetActiveProfileFor(sessionID, name)
	}
	if e = identity.SaveUserConfig(user); e != nil {
		// The registration itself is already durably recorded above --
		// only the local profile-registry entry failed to save. Surface
		// this rather than discarding it silently: the whole managed
		// project lifecycle upgrade system (agent-comms update apply,
		// project upgrade --all-known) depends on this registry to know
		// which projects exist at all, so a silently-lost entry here means
		// this project would never be reconciled automatically.
		return event, fmt.Errorf("registration recorded but could not save the identity profile registry entry: %w", e)
	}
	return event, nil
}

// CanSponsorRegistration reports whether actor is authorized to register a
// new agent under a different id on its behalf: an active ORCHESTRATOR-role
// principal, or any active HUMAN principal (which covers the project owner
// by construction — Register at init always creates it as PrincipalHuman).
// Self-registration (id == actor) never needs this check: every principal,
// registered or not, may always bootstrap its own identity.
func (s *Service) CanSponsorRegistration(actor string) (bool, error) {
	state, err := s.State()
	if err != nil {
		return false, err
	}
	principal, ok := state.Agents[actor]
	return ok && principal.Status == "ACTIVE" &&
		(principal.Role == model.RoleOrchestrator || principal.PrincipalType == model.PrincipalHuman), nil
}
func (s *Service) RotateKey(actor string) (model.Event, error) {
	cfg, e := s.Store.Config()
	if e != nil {
		return model.Event{}, e
	}
	old, e := identity.ResolveCredential(s.Store.Credentials, cfg.ProjectID, actor)
	if e != nil {
		return model.Event{}, e
	}
	next, e := identity.Generate(cfg.ProjectID, actor)
	if e != nil {
		return model.Event{}, e
	}
	payload := model.AgentKeyRotated{PublicKey: next.PublicKey, PreviousFingerprint: identity.Fingerprint(old.PublicKey)}
	raw, encodeErr := model.EncodePayload("agent.rotate-key", payload)
	if encodeErr != nil {
		return model.Event{}, encodeErr
	}
	command := controlplane.Command{
		ProjectID: cfg.ProjectID, Actor: actor, Type: "agent.rotate-key", EntityID: actor,
		Payload: raw, IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
	}
	if encodeErr = command.Sign(old.PrivateKey); encodeErr != nil {
		return model.Event{}, encodeErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlplane.DefaultRequestTimeout)
	defer cancel()
	remoteEvent, metadata, remoteErr := s.remote.Command(ctx, command)
	if remoteErr != nil {
		return model.Event{}, remoteErr
	}
	ev := model.Event{
		SchemaVersion: model.SchemaVersion, PayloadVersion: 1, ID: remoteEvent.ID,
		Sequence: remoteEvent.Sequence, Time: remoteEvent.Time, Actor: remoteEvent.Actor,
		Type: remoteEvent.Type, EntityID: remoteEvent.EntityID, Data: remoteEvent.Payload,
		PreviousHash: remoteEvent.PreviousHash, Hash: remoteEvent.Hash, Signature: command.Signature,
		KeyFingerprint: remoteEvent.ActorKeyFingerprint, ServerReceipt: metadata.Receipt,
		Consistency: metadata.Consistency, Connectivity: metadata.Connectivity,
	}
	if e = s.Store.Credentials.Put(next); e != nil {
		return ev, fmt.Errorf("key rotation recorded but new credential could not be stored: %w", e)
	}
	return ev, nil
}

// ElevateKey registers (or, by calling it again, rotates -- there is no
// separate recovery path if the passphrase is lost) a fresh,
// passphrase-encrypted elevated keypair for actor. The agent.elevate-key
// event is self-signed with actor's ordinary, unprotected primary
// credential -- proving "the primary key holder authorized this new
// elevated key" is exactly the intended semantics, not a weakness: the
// elevated key doesn't exist as a valid signer for anything until this
// event lands. The elevated credential is then stored locally under
// identity.ElevatedActor(actor), distinct from the primary one.
func (s *Service) ElevateKey(actor, passphrase string) (model.Event, error) {
	cfg, e := s.Store.Config()
	if e != nil {
		return model.Event{}, e
	}
	primary, e := identity.ResolveCredential(s.Store.Credentials, cfg.ProjectID, actor)
	if e != nil {
		return model.Event{}, e
	}
	elevated, e := identity.GenerateEncrypted(cfg.ProjectID, identity.ElevatedActor(actor), passphrase)
	if e != nil {
		return model.Event{}, e
	}
	payload := model.AgentElevatedKeyRegistered{PublicKey: elevated.PublicKey}
	event, e := s.executeRemoteWithCredential(cfg, actor, "agent.elevate-key", actor, payload, primary)
	if e != nil {
		return model.Event{}, e
	}
	if e = s.Store.Credentials.Put(elevated); e != nil {
		return event, fmt.Errorf("elevated key recorded but could not be stored locally: %w", e)
	}
	return event, nil
}

// DeleteProjectResult reports exactly what DeleteProject did, itemized --
// see RFC 0020. If RemoteDeleted is true but a later step fails, remote
// state is already, correctly, gone (nothing can undo that); the result
// still reports it faithfully alongside whatever local cleanup did or
// didn't finish, rather than presenting a false full success or a false
// full failure.
type DeleteProjectResult struct {
	ProjectID        string   `json:"project_id"`
	RuntimeMode      string   `json:"runtime_mode"`
	RemoteDeleted    bool     `json:"remote_deleted"`
	DaemonStopped    bool     `json:"daemon_stopped"`
	KeyringRemoved   []string `json:"keyring_removed,omitempty"`
	ProfilesRemoved  []string `json:"profiles_removed,omitempty"`
	RuntimeRemoved   bool     `json:"runtime_removed"`
	BootstrapRemoved bool     `json:"bootstrap_removed"`
	Warnings         []string `json:"warnings,omitempty"`
}

// DeleteProject permanently destroys the project this Service is opened
// against -- local runtime state always, and (in service mode) this
// project's entire row set on the shared authority too. See RFC 0020.
// OWNER-only, checked from live state, not just client-side trust;
// requires a registered elevated key; confirmDirectoryName must equal the
// project root's actual directory name (filepath.Base(s.Store.Root)) --
// the CLI/TUI's own typed-confirmation step, enforced here once centrally
// rather than trusted from every call site. Deliberately the directory
// name, not the internal project ID: the ID is an opaque generated UUID
// nobody has memorized, so typing it back would just mean copying it from
// the very screen that already displays it -- brittle to transcribe
// exactly and no more likely to be actually *read* than any other string.
// The directory name is something the human already knows without
// looking anything up (it's the folder they're standing in), and is
// exactly as effective at catching "right person, wrong project" -- the
// one mistake this step exists to catch, distinct from what the
// passphrase below proves. There is no automatic backup: by explicit
// design, this is unrecoverable.
//
// Ordering matters: remote deletion (service mode only) happens first,
// before anything local is touched, because local state is the only
// record that a retry is even needed -- it must never be discarded before
// remote deletion is confirmed. The daemon is stopped next, before any
// local file removal, because the daemon (personal mode especially) may
// still hold the runtime directory's files open.
func (s *Service) DeleteProject(actor, passphrase, confirmDirectoryName string) (DeleteProjectResult, error) {
	cfg, err := s.Store.Config()
	if err != nil {
		return DeleteProjectResult{}, err
	}
	result := DeleteProjectResult{ProjectID: cfg.ProjectID, RuntimeMode: cfg.RuntimeMode}
	state, err := s.State()
	if err != nil {
		return DeleteProjectResult{}, err
	}
	if state.Agents[actor].Role != model.RoleOwner {
		return DeleteProjectResult{}, fmt.Errorf("project delete: only the project owner can delete a project (actor: %s)", actor)
	}
	directoryName := filepath.Base(s.Store.Root)
	if confirmDirectoryName != directoryName {
		return DeleteProjectResult{}, fmt.Errorf("project delete: typed confirmation %q does not match the project directory name %q", confirmDirectoryName, directoryName)
	}
	elevated, err := s.Store.Credentials.Get(cfg.ProjectID, identity.ElevatedActor(actor))
	if err != nil {
		return DeleteProjectResult{}, fmt.Errorf(
			"project delete: %s has no registered elevated key -- run `agent-comms agent elevate-key` first: %w", actor, err)
	}
	credential, err := s.decryptElevatedKey(elevated, actor, "deleting project "+cfg.ProjectID, passphrase)
	if err != nil {
		return DeleteProjectResult{}, err
	}
	command := controlplane.Command{
		ProjectID: cfg.ProjectID, Actor: actor, Type: "project.delete", EntityID: cfg.ProjectID,
		IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
	}
	if err = command.Sign(credential.PrivateKey); err != nil {
		return DeleteProjectResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlplane.DefaultRequestTimeout)
	defer cancel()
	if cfg.RuntimeMode == "service" {
		client, clientErr := remote.NewWithToken(cfg.AuthorityURL, controlplane.DefaultRequestTimeout, strings.TrimSpace(os.Getenv("AGENT_COMMS_AUTHORITY_TOKEN")))
		if clientErr != nil {
			return DeleteProjectResult{}, clientErr
		}
		capabilities, capsErr := client.Capabilities(ctx)
		if capsErr != nil {
			return DeleteProjectResult{}, fmt.Errorf("project delete: checking authority capabilities: %w", capsErr)
		}
		if schemaVersion, _ := capabilities["schema_version"].(float64); schemaVersion < 4 {
			return DeleteProjectResult{}, errors.New(
				"project delete: the authority server does not yet support project deletion -- it needs upgrading first")
		}
		if deleteErr := client.DeleteProject(ctx, command); deleteErr != nil {
			return DeleteProjectResult{}, fmt.Errorf("project delete: %w", deleteErr)
		}
		result.RemoteDeleted = true
	}

	// Everything below is local cleanup. Remote state (if any) is already
	// gone at this point and cannot be undone -- failures from here on are
	// reported as warnings on a still-successful result, not returned as
	// errors, so a partial local cleanup is never mistaken for "nothing
	// happened, safe to retry the whole thing."
	daemonStopped, stopErr := projectlifecycle.StopDaemon(ctx, cfg)
	if stopErr != nil {
		result.Warnings = append(result.Warnings, "stop daemon: "+stopErr.Error())
	}
	result.DaemonStopped = daemonStopped

	actors := make(map[string]bool)
	for id := range state.Agents {
		actors[id] = true
	}
	if config, cfgErr := identity.LoadUserConfig(); cfgErr != nil {
		result.Warnings = append(result.Warnings, "load local identity profiles: "+cfgErr.Error())
	} else {
		updated, removedProfiles := config.RemoveProfilesForProject(cfg.ProjectID)
		for _, p := range removedProfiles {
			actors[p.Actor] = true
			result.ProfilesRemoved = append(result.ProfilesRemoved, p.Name)
		}
		if saveErr := identity.SaveUserConfig(updated); saveErr != nil {
			result.Warnings = append(result.Warnings, "save local identity profiles: "+saveErr.Error())
		}
	}
	for id := range actors {
		if deleteErr := s.Store.Credentials.Delete(cfg.ProjectID, id); deleteErr == nil {
			result.KeyringRemoved = append(result.KeyringRemoved, id)
		}
		if deleteErr := s.Store.Credentials.Delete(cfg.ProjectID, identity.ElevatedActor(id)); deleteErr == nil {
			result.KeyringRemoved = append(result.KeyringRemoved, identity.ElevatedActor(id))
		}
	}
	sort.Strings(result.KeyringRemoved)
	sort.Strings(result.ProfilesRemoved)

	if removeErr := removeRuntimeDirectoryWithRetry(filepath.Join(s.Store.Root, store.Runtime)); removeErr != nil {
		result.Warnings = append(result.Warnings, "remove runtime directory: "+removeErr.Error())
	} else {
		result.RuntimeRemoved = true
	}
	if removeErr := os.Remove(filepath.Join(s.Store.Root, ".agents")); removeErr != nil && !os.IsNotExist(removeErr) {
		result.Warnings = append(result.Warnings, "remove .agents: "+removeErr.Error())
	} else {
		result.BootstrapRemoved = true
	}
	return result, nil
}

// removeRuntimeDirectoryWithRetry retries os.RemoveAll briefly instead of
// failing on the first attempt. StopDaemon above confirms the daemon stops
// answering health checks, but that only proves its HTTP listener closed --
// the personal-authority SQLite file underneath (internal/daemon/run.go)
// is closed by a deferred Close() that only runs once Run() itself
// returns, strictly after the health check already started failing. POSIX
// allows unlinking a still-open file outright, so this race never
// surfaces there; Windows refuses to remove a file another process still
// has open at all ("The process cannot access the file because it is
// being used by another process"), confirmed live in CI. The retry
// budget roughly matches internal/daemon's own daemonShutdownTimeout (10s)
// -- generous for the rare slow case, a no-op cost everywhere else since
// it returns on the first successful attempt.
func removeRuntimeDirectoryWithRetry(path string) error {
	var lastErr error
	for attempt := 0; attempt < 40; attempt++ {
		if lastErr = os.RemoveAll(path); lastErr == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return lastErr
}

func (s *Service) AddArtifact(actor, path string) (model.Event, error) {
	cfg, e := s.Store.Config()
	if e != nil {
		return model.Event{}, e
	}
	f, e := os.Open(path)
	if e != nil {
		return model.Event{}, e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, f)
	if e != nil {
		return model.Event{}, e
	}
	settings := model.DefaultProjectSettings()
	if state, stateErr := s.State(); stateErr == nil {
		settings = model.EffectiveProjectSettings(state.ProjectSettings)
	} else if cfg.ArtifactLimitBytes > 0 {
		settings.ArtifactLimitBytes = cfg.ArtifactLimitBytes
	}
	storage := "local"
	if n > settings.ArtifactLimitBytes {
		return model.Event{}, fmt.Errorf("artifact exceeds the project limit of %d bytes", settings.ArtifactLimitBytes)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	dst := filepath.Join(s.Store.Root, store.Runtime, "artifacts", "sha256", sum)
	b, e := os.ReadFile(path)
	if e != nil {
		return model.Event{}, e
	}
	if e = os.WriteFile(dst, b, 0600); e != nil {
		return model.Event{}, e
	}
	mt := mime.TypeByExtension(filepath.Ext(path))
	if mt == "" {
		mt = "application/octet-stream"
	}
	return s.Execute(actor, "artifact.add", sum, model.ArtifactAdded{SHA256: sum, Size: n, Name: filepath.Base(path), MediaType: mt, Storage: storage})
}
func (s *Service) Archive(actor string) (model.Event, error) {
	st, e := s.State()
	if e != nil {
		return model.Event{}, e
	}
	before := time.Now().UTC().Add(-7 * 24 * time.Hour)
	ids := []string{}
	for id, t := range st.Tasks {
		if t.CompletedAt != nil && t.CompletedAt.Before(before) && !t.Archived {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return s.Execute(actor, "archive.run", "archive", model.ArchiveRun{Before: before, TaskIDs: ids})
}
func (s *Service) ExportJSONL(w io.Writer) error {
	if e := s.Verify(0, 0); e != nil {
		return e
	}
	enc := json.NewEncoder(w)
	cursor := ""
	for {
		page, e := s.History(controlplane.PageRequest{Cursor: cursor, Limit: controlplane.MaxPageSize})
		if e != nil {
			return e
		}
		for _, record := range page.Items {
			if e = enc.Encode(record); e != nil {
				return e
			}
		}
		if page.NextCursor == "" {
			return nil
		}
		cursor = page.NextCursor
	}
}
func (s *Service) ExportMarkdown(w io.Writer) error {
	st, e := s.State()
	if e != nil {
		return e
	}
	fmt.Fprintf(w, "# Agent Comms audit report\n\nIntegrity: **%t** · Events: %d · Head: `%s`\n\n## Tasks\n\n", st.Integrity.Verified, st.Integrity.EventCount, st.Integrity.Head)
	for _, id := range SortedKeys(st.Tasks) {
		t := st.Tasks[id]
		fmt.Fprintf(w, "- **%s** — %s · owner: %s · resources: %s\n", id, t.Status, t.Owner, strings.Join(t.Resources, ", "))
	}
	fmt.Fprint(w, "\n## Decisions\n\n")
	for _, id := range SortedKeys(st.Documents) {
		d := st.Documents[id]
		isDecision := false
		for _, tag := range d.Tags {
			if tag == "decision" {
				isDecision = true
			}
		}
		if isDecision {
			fmt.Fprintf(w, "- **%s** — %s: %s\n", id, d.Title, d.Body)
		}
	}
	return nil
}
func SortedKeys[V any](m map[string]V) []string {
	o := make([]string, 0, len(m))
	for k := range m {
		o = append(o, k)
	}
	sort.Strings(o)
	return o
}
