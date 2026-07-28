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
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/google/uuid"
)

type Service struct {
	Store         *store.Store
	remote        *daemonclient.Client
	remoteErr     error
	recoverRemote func() error
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
	state, metadata, err := s.remote.State(ctx, cfg.ProjectID)
	if err != nil {
		return state, err
	}
	state.Integrity.Consistency = metadata.Consistency
	state.Integrity.ServerSequence = metadata.ServerSequence
	state.Integrity.CacheSequence = metadata.CacheSequence
	state.Integrity.Connectivity = metadata.Connectivity
	protocol.RefreshRuntimePresence(&state, time.Now().UTC())
	return state, nil
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
	if runtimeID != "" {
		runtime, exists := state.AgentRuntimes[runtimeID]
		if !exists || runtime.AgentID != actor || runtime.Status == "DRAINING" ||
			runtime.Status == "REVOKED" || len(runtime.ActiveInvocations) >= runtime.MaxConcurrent {
			return model.Invocation{}, false
		}
	}
	var selected model.Invocation
	found := false
	priorityRank := map[string]int{"URGENT": 4, "HIGH": 3, "NORMAL": 2, "LOW": 1}
	for _, invocation := range state.Invocations {
		if invocation.Target != actor || (invocation.Status != "PENDING" && invocation.Status != "NOTIFIED") {
			continue
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
	if s.remoteErr != nil {
		return model.Event{}, s.remoteErr
	}
	return s.executeRemote(actor, typ, id, payload)
}

func (s *Service) executeRemote(actor, typ, id string, payload any) (model.Event, error) {
	cfg, err := s.Store.Config()
	if err != nil {
		return model.Event{}, err
	}
	credential, err := identity.ResolveCredential(s.Store.Credentials, cfg.ProjectID, actor)
	if err != nil {
		return model.Event{}, fmt.Errorf("credential for %s: %w", actor, err)
	}
	return s.executeRemoteWithCredential(cfg, actor, typ, id, payload, credential)
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
	for attempt := 0; attempt < 3; attempt++ {
		event, metadata, err = s.remote.Command(ctx, command)
		if err == nil {
			break
		}
		var controlErr *controlplane.Error
		if !errors.As(err, &controlErr) ||
			(controlErr.Code != controlplane.CodeOffline && controlErr.Code != controlplane.CodeUnavailable &&
				controlErr.Code != controlplane.CodeRateLimited) {
			break
		}
		if s.recoverRemote != nil &&
			(controlErr.Code == controlplane.CodeOffline || controlErr.Code == controlplane.CodeUnavailable) {
			if recoveryErr := s.recoverRemote(); recoveryErr != nil {
				err = recoveryErr
				break
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
			err = ctx.Err()
			attempt = 3
		case <-timer.C:
		}
	}
	if err != nil {
		return model.Event{}, err
	}
	return model.Event{
		SchemaVersion: model.SchemaVersion, PayloadVersion: 1, ID: event.ID,
		Sequence: event.Sequence, Time: event.Time, Actor: event.Actor, Type: event.Type,
		EntityID: event.EntityID, Data: event.Payload, PreviousHash: event.PreviousHash,
		Hash: event.Hash, Signature: command.Signature,
		KeyFingerprint: identity.Fingerprint(credential.PublicKey),
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
	if user.ActiveProfile == "" {
		user.ActiveProfile = name
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
		KeyFingerprint: identity.Fingerprint(old.PublicKey), ServerReceipt: metadata.Receipt,
		Consistency: metadata.Consistency, Connectivity: metadata.Connectivity,
	}
	if e = s.Store.Credentials.Put(next); e != nil {
		return ev, fmt.Errorf("key rotation recorded but new credential could not be stored: %w", e)
	}
	return ev, nil
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
	for _, id := range SortedKeys(st.Decisions) {
		d := st.Decisions[id]
		fmt.Fprintf(w, "- **%s** — %s: %s\n", id, d.Title, d.Statement)
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
