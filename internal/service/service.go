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
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/google/uuid"
)

type Service struct {
	Store     *store.Store
	remote    *daemonclient.Client
	remoteErr error
}

func New(root string) *Service {
	instance := &Service{Store: store.Open(root)}
	cfg, err := instance.Store.Config()
	if err != nil || cfg.RuntimeMode != "service" {
		return instance
	}
	if cfg.DaemonEndpoint == "" {
		instance.remoteErr = errors.New("service runtime is missing daemon endpoint")
		return instance
	}
	instance.remote, instance.remoteErr = daemonclient.New(cfg.DaemonEndpoint, controlplane.DefaultRequestTimeout)
	return instance
}
func (s *Service) State() (model.State, error) {
	if s.remoteErr != nil {
		return model.State{}, s.remoteErr
	}
	if s.remote != nil {
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
		return state, nil
	}
	ev, e := s.Store.Events()
	if e != nil {
		return model.State{}, e
	}
	st := model.State{
		Agents: map[string]model.Agent{}, Tasks: map[string]model.Task{}, Messages: map[string]model.Message{},
		Invocations: map[string]model.Invocation{}, InvocationDeliveries: map[string]model.InvocationDelivery{},
		AgentRuntimes: map[string]model.AgentRuntime{}, InvocationPolicies: map[string]model.InvocationPolicy{},
		Approvals: map[string]model.Approval{}, Decisions: map[string]model.Decision{},
		Documents: map[string]model.Document{}, Env: map[string]model.EnvEntry{},
		Sessions: map[string]model.SessionPayload{}, Artifacts: map[string]model.Artifact{},
	}
	unknown := 0
	for _, v := range ev {
		if !model.KnownEventType(v.Type) {
			unknown++
			continue
		}
		if e = ApplyEvent(&st, v); e != nil {
			return st, fmt.Errorf("project event %s: %w", v.ID, e)
		}
	}
	st.Integrity = model.Integrity{Verified: s.Store.Verify() == nil, EventCount: len(ev), Head: s.Store.Head(), UnknownEvents: unknown, Remote: s.Store.Remote(), SyncState: "local-only"}
	if st.Integrity.Remote != "" {
		st.Integrity.SyncState = "configured"
	}
	return st, nil
}

func (s *Service) Verify(from, to uint64) error {
	if s.remoteErr != nil {
		return s.remoteErr
	}
	if s.remote != nil {
		cfg, err := s.Store.Config()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), controlplane.DefaultRequestTimeout)
		defer cancel()
		return s.remote.Verify(ctx, cfg.ProjectID, from, to)
	}
	return s.Store.Verify()
}

func (s *Service) NextInvocation(actor, runtimeID string) (model.Invocation, bool, error) {
	state, err := s.State()
	if err != nil {
		return model.Invocation{}, false, err
	}
	invocation, found := SelectNextInvocation(state, actor, runtimeID, time.Now().UTC())
	return invocation, found, nil
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
	if s.remote != nil {
		cfg, err := s.Store.Config()
		if err != nil {
			return controlplane.EventPage{}, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), controlplane.DefaultRequestTimeout)
		defer cancel()
		return s.remote.Events(ctx, cfg.ProjectID, page)
	}
	limit, err := page.BoundedLimit()
	if err != nil {
		return controlplane.EventPage{}, err
	}
	after, err := controlplane.DecodeCursor(page.Cursor)
	if err != nil {
		return controlplane.EventPage{}, err
	}
	events, err := s.Store.Events()
	if err != nil {
		return controlplane.EventPage{}, err
	}
	items := make([]controlplane.EventRecord, 0, limit)
	for _, event := range events {
		if event.Sequence <= after {
			continue
		}
		items = append(items, controlplane.EventRecord{Event: controlplane.Event{
			ProjectID: "", Sequence: event.Sequence, ID: event.ID, Time: event.Time,
			Actor: event.Actor, Type: event.Type, EntityID: event.EntityID, Payload: event.Data,
			PreviousHash: event.PreviousHash, Hash: event.Hash,
			ActorIntentHash: event.Hash, IdempotencyKey: "legacy:" + event.ID, Legacy: true,
		}})
		if len(items) > limit {
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
			Consistency: "LEGACY_LOCAL", CacheSequence: last, Connectivity: "LOCAL",
		},
	}, nil
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
func ApplyEvent(s *model.State, e model.Event) error {
	if s.Invocations == nil {
		s.Invocations = map[string]model.Invocation{}
	}
	if s.InvocationDeliveries == nil {
		s.InvocationDeliveries = map[string]model.InvocationDelivery{}
	}
	if s.AgentRuntimes == nil {
		s.AgentRuntimes = map[string]model.AgentRuntime{}
	}
	if s.InvocationPolicies == nil {
		s.InvocationPolicies = map[string]model.InvocationPolicy{}
	}
	v, x := model.DecodePayload(e.Type, e.Data)
	if x != nil {
		return x
	}
	switch p := v.(type) {
	case *model.AgentRegistered:
		s.Agents[e.EntityID] = model.Agent{ID: e.EntityID, DisplayName: p.DisplayName, Status: "PENDING", PrincipalType: p.PrincipalType, PublicKey: p.PublicKey, KeyFingerprint: identity.Fingerprint(p.PublicKey)}
	case *model.AgentActivated:
		a := s.Agents[e.EntityID]
		a.Status = "ACTIVE"
		a.Role = p.Role
		a.Capabilities = p.Capabilities
		a.Scopes = p.Scopes
		s.Agents[e.EntityID] = a
	case *model.AgentKeyRotated:
		a := s.Agents[e.EntityID]
		a.PublicKey = p.PublicKey
		a.KeyFingerprint = identity.Fingerprint(p.PublicKey)
		s.Agents[e.EntityID] = a
	case *model.TaskCreated:
		s.Tasks[e.EntityID] = model.Task{ID: e.EntityID, Title: p.Title, Summary: p.Summary, Status: "OPEN", Repository: p.Repository, Branch: p.Branch, Worktree: p.Worktree, Resources: p.Resources, ExternalRef: p.ExternalRef, Risk: defaultRisk(p.Risk)}
	case *model.TaskOffered:
		t := s.Tasks[e.EntityID]
		t.Status = "OFFERED"
		t.Offers = append(t.Offers, model.Offer{ID: e.ID, To: p.To, ExpiresAt: p.ExpiresAt, Status: "PENDING"})
		s.Tasks[e.EntityID] = t
	case *model.TaskClaimed:
		t := s.Tasks[e.EntityID]
		t.Owner = e.Actor
		t.Status = "CLAIMED"
		t.LeaseUntil = p.LeaseUntil
		t.StaleUntil = p.LeaseUntil.Add(time.Hour)
		if p.Worktree != "" {
			t.Worktree = p.Worktree
		}
		for i := range t.Offers {
			if t.Offers[i].To == e.Actor && t.Offers[i].Status == "PENDING" {
				t.Offers[i].Status = "ACCEPTED"
			}
		}
		s.Tasks[e.EntityID] = t
	case *model.TaskRenewed:
		t := s.Tasks[e.EntityID]
		t.LeaseUntil = p.LeaseUntil
		t.StaleUntil = p.LeaseUntil.Add(time.Hour)
		s.Tasks[e.EntityID] = t
	case *model.TaskHandoff:
		t := s.Tasks[e.EntityID]
		t.HandoffTo = p.To
		s.Tasks[e.EntityID] = t
	case *model.TaskStatus:
		if e.Type == "agent.suspend" {
			a := s.Agents[e.EntityID]
			a.Status = "SUSPENDED"
			s.Agents[e.EntityID] = a
			break
		}
		t := s.Tasks[e.EntityID]
		switch e.Type {
		case "task.start":
			t.Status = "IN_PROGRESS"
		case "task.block":
			t.Status = "BLOCKED"
		case "task.review":
			t.Status = "REVIEW"
		case "task.complete":
			t.Status = "COMPLETED"
			z := e.Time
			t.CompletedAt = &z
		case "task.cancel":
			t.Status = "CANCELLED"
		case "task.handoff.accept":
			t.Owner = e.Actor
			t.HandoffTo = ""
		case "task.takeover":
			t.Owner = e.Actor
			t.Status = "CLAIMED"
			t.LeaseUntil = e.Time.Add(4 * time.Hour)
			t.StaleUntil = t.LeaseUntil.Add(time.Hour)
		}
		s.Tasks[e.EntityID] = t
	case *model.MessagePosted:
		r := make([]model.RecipientState, len(p.To))
		for i, to := range p.To {
			r[i] = model.RecipientState{Principal: to, Status: initialRecipientStatus(p.Kind)}
		}
		status := "OPEN"
		if p.Kind == "FYI" {
			status = "DELIVERED"
		}
		s.Messages[e.EntityID] = model.Message{ID: e.EntityID, Kind: p.Kind, From: e.Actor, To: p.To, Subject: p.Subject, Body: p.Body, TaskID: p.TaskID, Status: status, Recipients: r}
	case *model.MessageResponse:
		m := s.Messages[e.EntityID]
		for i := range m.Recipients {
			if m.Recipients[i].Principal == e.Actor {
				z := e.Time
				m.Recipients[i].Status = p.Response
				m.Recipients[i].At = &z
			}
		}
		m.Status = messageStatus(m)
		s.Messages[e.EntityID] = m
		if p.Response == "RESOLVED" && m.TaskID != "" {
			if t, ok := s.Tasks[m.TaskID]; ok && t.Status == "BLOCKED" {
				t.Status = "OPEN"
				s.Tasks[m.TaskID] = t
			}
		}
	case *model.InvocationRequested:
		priority := strings.ToUpper(strings.TrimSpace(p.Priority))
		if priority == "" {
			priority = "NORMAL"
		}
		s.Invocations[e.EntityID] = model.Invocation{
			ID: e.EntityID, RequestedBy: e.Actor, Target: p.Target, MessageID: p.MessageID,
			TaskID: p.TaskID, Instruction: p.Instruction, ExpectedResult: p.ExpectedResult,
			Priority: priority, Status: "PENDING", CreatedAt: e.Time, Deadline: p.Deadline,
		}
	case *model.InvocationNotified:
		invocation := s.Invocations[e.EntityID]
		now := e.Time
		invocation.Status = "NOTIFIED"
		s.Invocations[e.EntityID] = invocation
		s.InvocationDeliveries[p.DeliveryID] = model.InvocationDelivery{
			ID: p.DeliveryID, InvocationID: e.EntityID, RuntimeID: p.RuntimeID,
			Attempt: p.Attempt, Status: "NOTIFIED", NotifiedAt: &now,
		}
	case *model.InvocationClaimed:
		invocation := s.Invocations[e.EntityID]
		now := p.ClaimUntil
		invocation.Status = "CLAIMED"
		invocation.ClaimedBy = e.Actor
		invocation.RuntimeID = p.RuntimeID
		invocation.ClaimUntil = &now
		s.Invocations[e.EntityID] = invocation
	case *model.InvocationProgress:
		invocation := s.Invocations[e.EntityID]
		invocation.Status = "RUNNING"
		if invocation.StartedAt == nil {
			now := e.Time
			invocation.StartedAt = &now
		}
		invocation.Summary = p.Summary
		invocation.Reason = ""
		invocation.NextAttemptAt = nil
		s.Invocations[e.EntityID] = invocation
	case *model.InvocationWaiting:
		invocation := s.Invocations[e.EntityID]
		invocation.Status = "WAITING"
		invocation.Reason = p.Reason
		invocation.NextAttemptAt = p.NextAttemptAt
		s.Invocations[e.EntityID] = invocation
	case *model.InvocationCompleted:
		invocation := s.Invocations[e.EntityID]
		now := e.Time
		invocation.Status = "COMPLETED"
		invocation.CompletedAt = &now
		invocation.ResultMessageID = p.ResultMessageID
		invocation.Summary = p.Summary
		s.Invocations[e.EntityID] = invocation
	case *model.InvocationRejected:
		invocation := s.Invocations[e.EntityID]
		now := e.Time
		if e.Type == "invocation.expire" {
			invocation.Status = "EXPIRED"
		} else {
			invocation.Status = "REJECTED"
		}
		invocation.CompletedAt = &now
		invocation.Reason = p.Reason
		s.Invocations[e.EntityID] = invocation
	case *model.InvocationDeliveryFailed:
		invocation := s.Invocations[e.EntityID]
		now := e.Time
		status := "FAILED"
		if p.Final {
			status = "DEAD_LETTER"
			invocation.Status = status
			invocation.CompletedAt = &now
			invocation.Reason = p.Error
		} else {
			invocation.Status = "PENDING"
		}
		invocation.NextAttemptAt = p.NextRetry
		s.Invocations[e.EntityID] = invocation
		s.InvocationDeliveries[p.DeliveryID] = model.InvocationDelivery{
			ID: p.DeliveryID, InvocationID: e.EntityID, RuntimeID: p.RuntimeID,
			Attempt: p.Attempt, Status: status, FailedAt: &now, NextRetryAt: p.NextRetry, Error: p.Error,
		}
	case *model.RuntimeRegistered:
		s.AgentRuntimes[e.EntityID] = model.AgentRuntime{
			ID: e.EntityID, AgentID: p.AgentID, Connector: p.Connector,
			ConfigReference: p.ConfigReference, Status: "OFFLINE", Health: "UNKNOWN",
			MaxConcurrent: p.MaxConcurrent, Scopes: p.Scopes, Capabilities: p.Capabilities,
			RegisteredAt: e.Time, LastChangedBy: e.Actor,
		}
	case *model.RuntimeHeartbeat:
		runtime := s.AgentRuntimes[e.EntityID]
		runtime.Status = "ONLINE"
		runtime.Health = p.Health
		runtime.ActiveInvocations = p.ActiveInvocations
		runtime.LastSeenAt = e.Time
		runtime.LastChangedBy = e.Actor
		runtime.Reason = ""
		s.AgentRuntimes[e.EntityID] = runtime
	case *model.RuntimeStatusChanged:
		runtime := s.AgentRuntimes[e.EntityID]
		switch e.Type {
		case "runtime.drain":
			runtime.Status = "DRAINING"
		case "runtime.resume":
			runtime.Status = "OFFLINE"
		case "runtime.revoke":
			runtime.Status = "REVOKED"
		}
		runtime.Reason = p.Reason
		runtime.LastChangedBy = e.Actor
		s.AgentRuntimes[e.EntityID] = runtime
	case *model.InvocationPolicyUpdated:
		s.InvocationPolicies[e.EntityID] = model.InvocationPolicy{
			AgentID: e.EntityID, Mode: p.Mode, TrustedActors: p.TrustedActors,
			AllowedScopes: p.AllowedScopes, RequireHumanForSensitive: p.RequireHumanForSensitive,
			UpdatedBy: e.Actor, UpdatedAt: e.Time,
		}
	case *model.ApprovalRequested:
		s.Approvals[e.EntityID] = model.Approval{ID: e.EntityID, Tier: p.Tier, Action: p.Action, Reason: p.Reason, Status: "PENDING", Requester: e.Actor, Affected: p.Affected}
	case *model.ApprovalResponse:
		a := s.Approvals[e.EntityID]
		if e.Type == "approval.approve" {
			a.Status = "APPROVED"
		} else {
			a.Status = "REJECTED"
		}
		a.Approver = e.Actor
		s.Approvals[e.EntityID] = a
	case *model.DecisionPayload:
		s.Decisions[e.EntityID] = model.Decision{ID: e.EntityID, Title: p.Title, Statement: p.Statement, Supersedes: p.Supersedes, To: p.To, Status: "ACTIVE"}
		if p.Supersedes != "" {
			d := s.Decisions[p.Supersedes]
			d.Status = "SUPERSEDED"
			s.Decisions[p.Supersedes] = d
		}
	case *model.SessionPayload:
		if e.Type == "session.start" {
			s.Sessions[e.EntityID] = *p
		} else {
			delete(s.Sessions, e.EntityID)
		}
	case *model.ArtifactAdded:
		s.Artifacts[p.SHA256] = model.Artifact{SHA256: p.SHA256, Size: p.Size, Name: p.Name, MediaType: p.MediaType, Storage: p.Storage}
	case *model.ArchiveRun:
		for _, id := range p.TaskIDs {
			t := s.Tasks[id]
			t.Archived = true
			s.Tasks[id] = t
		}
	case *model.DocumentPayload:
		switch e.Type {
		case "document.create":
			s.Documents[e.EntityID] = model.Document{ID: e.EntityID, Title: p.Title, Body: p.Body, Tags: p.Tags, Status: "ACTIVE", Version: 1, Author: e.Actor}
		case "document.update":
			d := s.Documents[e.EntityID]
			d.Title = p.Title
			d.Body = p.Body
			d.Tags = p.Tags
			d.Version++
			s.Documents[e.EntityID] = d
		case "document.supersede":
			d := s.Documents[e.EntityID]
			d.Status = "SUPERSEDED"
			s.Documents[e.EntityID] = d
			if p.ReplacementID != "" {
				nd := s.Documents[p.ReplacementID]
				nd.Status = "ACTIVE"
				nd.Supersedes = e.EntityID
				s.Documents[p.ReplacementID] = nd
			}
		}
	case *model.EnvSetPayload:
		s.Env[p.Key] = model.EnvEntry{Key: p.Key, Value: p.Value, UpdatedAt: e.Time, UpdatedBy: e.Actor}
	case *model.EnvDeletePayload:
		delete(s.Env, p.Key)
	}
	return nil
}
func defaultRisk(v string) string {
	if v == "" {
		return "ROUTINE"
	}
	return strings.ToUpper(v)
}
func initialRecipientStatus(kind string) string {
	if kind == "FYI" {
		return "DELIVERED"
	}
	return "PENDING"
}
func messageStatus(m model.Message) string {
	all := true
	anyReject := false
	for _, r := range m.Recipients {
		if r.Status == "REJECTED" {
			anyReject = true
		}
		if r.Status == "PENDING" {
			all = false
		}
	}
	if anyReject {
		return "REJECTED"
	}
	if all {
		return "SATISFIED"
	}
	return "OPEN"
}
func overlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y || x == "*" || y == "*" || strings.HasPrefix(x, y+"/") || strings.HasPrefix(y, x+"/") {
				return true
			}
		}
	}
	return false
}
func active(st model.State, actor string) (model.Agent, error) {
	a, ok := st.Agents[actor]
	if !ok || a.Status != "ACTIVE" {
		return a, errors.New("active principal required")
	}
	return a, nil
}
func elevated(typ string) bool {
	return typ == "approval.approve" || typ == "approval.reject" || typ == "agent.activate" || typ == "agent.suspend" || typ == "agent.rotate-key"
}
func hasApproval(st model.State, action string) bool {
	for _, a := range st.Approvals {
		if a.Action == action && a.Status == "APPROVED" {
			return true
		}
	}
	return false
}
func scopeAllows(scopes, resources []string) bool {
	for _, resource := range resources {
		allowed := false
		for _, scope := range scopes {
			if scope == "*" || scope == resource || strings.HasPrefix(resource, scope+"/") {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}
func (s *Service) Execute(actor, typ, id string, payload any) (model.Event, error) {
	if s.remoteErr != nil {
		return model.Event{}, s.remoteErr
	}
	if s.remote != nil {
		return s.executeRemote(actor, typ, id, payload)
	}
	if incomplete, status := s.Store.MigrationIncomplete(); incomplete && !migrationSeedEvent(typ) {
		return model.Event{}, fmt.Errorf("runtime migration is incomplete (%s); event %s is blocked", status, typ)
	}
	if incomplete, state := s.Store.CutoverIncomplete(); incomplete && !migrationSeedEvent(typ) {
		return model.Event{}, fmt.Errorf("migration cutover is incomplete (%s); event %s is blocked", state, typ)
	} else if !incomplete && state == store.CutoverActivated && !s.Store.ManagedBootstrapValid() {
		return model.Event{}, errors.New("split-brain cutover detected; durable work is blocked")
	}
	return s.Store.Mutate(actor, func() (string, string, any, error) {
		st, e := s.State()
		if e != nil {
			return "", "", nil, e
		}
		normalized, e := ValidateTransition(st, actor, typ, id, payload, time.Now().UTC())
		if e != nil {
			return "", "", nil, e
		}
		return typ, id, normalized, nil
	})
}

func (s *Service) executeRemote(actor, typ, id string, payload any) (model.Event, error) {
	cfg, err := s.Store.Config()
	if err != nil {
		return model.Event{}, err
	}
	raw, err := model.EncodePayload(typ, payload)
	if err != nil {
		return model.Event{}, err
	}
	credential, err := identity.ResolveCredential(s.Store.Credentials, cfg.ProjectID, actor)
	if err != nil {
		return model.Event{}, fmt.Errorf("credential for %s: %w", actor, err)
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
	event, metadata, err := s.remote.Command(ctx, command)
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

// ValidateTransition validates and normalizes one command against a current
// projection. Authoritative backends call this only after locking the project
// head inside the same transaction that appends the resulting event.
func ValidateTransition(st model.State, actor, typ, id string, payload any, now time.Time) (any, error) {
	if actor == "" {
		return nil, errors.New("actor is required")
	}
	if typ == "agent.register" {
		registered, ok := payload.(model.AgentRegistered)
		if !ok {
			return nil, errors.New("invalid agent registration payload")
		}
		if id == "" || registered.PublicKey == "" ||
			(registered.PrincipalType != model.PrincipalHuman && registered.PrincipalType != model.PrincipalAgent) {
			return nil, errors.New("agent ID, public key, and valid principal type are required")
		}
		if _, exists := st.Agents[id]; exists {
			return nil, errors.New("principal already exists")
		}
	}
	if typ != "agent.register" {
		a, x := active(st, actor)
		if x != nil {
			return nil, x
		}
		if elevated(typ) && a.Role != model.RoleOwner && a.Role != model.RoleOrchestrator {
			return nil, errors.New("owner or orchestrator role required")
		}
		if typ == "approval.approve" {
			ap := st.Approvals[id]
			if ap.Tier == "HUMAN" && a.PrincipalType != model.PrincipalHuman {
				return nil, errors.New("human principal required for this approval")
			}
		}
	}
	if typ == "agent.activate" {
		if _, ok := st.Agents[id]; !ok {
			return nil, errors.New("pending principal not found")
		}
		activation, ok := payload.(model.AgentActivated)
		if !ok || (activation.Role != model.RoleOwner && activation.Role != model.RoleOrchestrator &&
			activation.Role != model.RoleAgent && activation.Role != model.RoleObserver) {
			return nil, errors.New("valid activation role is required")
		}
	}
	if strings.HasPrefix(typ, "task.") {
		t, exists := st.Tasks[id]
		if typ != "task.create" && !exists {
			return nil, errors.New("task not found")
		}
		switch p := payload.(type) {
		case model.TaskCreated:
			if p.Title == "" || p.Repository == "" || p.Branch == "" || len(p.Resources) == 0 {
				return nil, errors.New("title, repository, branch, and resources are required")
			}
			if exists {
				return nil, errors.New("task already exists")
			}
		case model.TaskClaimed:
			a, _ := active(st, actor)
			if a.Role == model.RoleObserver {
				return nil, errors.New("observer cannot claim tasks")
			}
			if t.Owner != "" || (t.Status != "OPEN" && t.Status != "OFFERED") {
				return nil, errors.New("task is no longer available to claim")
			}
			if !scopeAllows(a.Scopes, t.Resources) {
				return nil, errors.New("task resources exceed principal scopes")
			}
			for _, v := range st.Tasks {
				if v.ID != id && v.Owner != "" && !v.Archived && v.Status != "COMPLETED" && v.Status != "CANCELLED" && overlap(t.Resources, v.Resources) {
					if !hasApproval(st, "shared-write:"+id+":"+v.ID) && !hasApproval(st, "shared-write:"+v.ID+":"+id) {
						return nil, fmt.Errorf("write lease overlaps task %s", v.ID)
					}
				}
			}
			p.LeaseUntil = now.Add(4 * time.Hour)
			if t.Worktree != "" && p.Worktree == "" {
				p.Worktree = t.Worktree
			}
			if p.Worktree != "" {
				for _, v := range st.Tasks {
					if v.Worktree == "" || v.Owner == "" || v.Archived || v.Status == "COMPLETED" || v.Status == "CANCELLED" {
						continue
					}
					if v.Worktree == p.Worktree && v.Owner != actor && v.LeaseUntil.After(now) {
						return nil, fmt.Errorf("worktree %s is already leased by %s (task %s, expires %s)", p.Worktree, v.Owner, v.ID, v.LeaseUntil.Local().Format("15:04"))
					}
				}
			}
			payload = p
		case model.TaskRenewed:
			if t.Owner != actor {
				return nil, errors.New("task owner required")
			}
			if strings.TrimSpace(p.Progress) == "" {
				return nil, errors.New("progress summary is required")
			}
			p.LeaseUntil = now.Add(4 * time.Hour)
			payload = p
		case model.TaskHandoff:
			if t.Owner != actor {
				return nil, errors.New("task owner required")
			}
		case model.TaskStatus:
			allowedStatus := map[string]map[string]bool{
				"task.start":    {"CLAIMED": true, "BLOCKED": true},
				"task.block":    {"IN_PROGRESS": true},
				"task.review":   {"IN_PROGRESS": true},
				"task.complete": {"IN_PROGRESS": true, "REVIEW": true},
				"task.cancel":   {"OPEN": true, "OFFERED": true, "CLAIMED": true, "IN_PROGRESS": true, "BLOCKED": true, "REVIEW": true},
			}
			if allowed, constrained := allowedStatus[typ]; constrained && !allowed[t.Status] {
				return nil, fmt.Errorf("%s is invalid while task is %s", typ, t.Status)
			}
			if typ == "task.takeover" && !hasApproval(st, "task.takeover:"+id) {
				return nil, errors.New("approved takeover is required")
			}
			if typ == "task.complete" && t.Risk != "ROUTINE" {
				if t.Status != "REVIEW" {
					return nil, errors.New("elevated task requires review before completion")
				}
				a, _ := active(st, actor)
				if a.Role != model.RoleOwner && a.Role != model.RoleOrchestrator {
					return nil, errors.New("eligible reviewer required")
				}
			}
			if typ == "task.handoff.accept" && t.HandoffTo != actor {
				return nil, errors.New("handoff target required")
			}
			if typ != "task.handoff.accept" && typ != "task.takeover" && t.Owner != "" && t.Owner != actor {
				a, _ := active(st, actor)
				if a.Role != model.RoleOwner && a.Role != model.RoleOrchestrator {
					return nil, errors.New("task owner or orchestrator required")
				}
			}
		}
	}
	if typ == "message.post" {
		p := payload.(model.MessagePosted)
		validKinds := []string{"FYI", "ACTION", "CONTRACT", "BLOCKER", "DECISION"}
		valid := make(map[string]bool, len(validKinds))
		for _, k := range validKinds {
			valid[k] = true
		}
		if !valid[p.Kind] || len(p.To) == 0 || p.Subject == "" {
			return nil, fmt.Errorf("valid kind (%s), recipient, and subject are required", strings.Join(validKinds, ", "))
		}
		if _, exists := st.Messages[id]; exists {
			return nil, errors.New("message already exists")
		}
		if len(p.To) > controlplane.MaxRecipients {
			return nil, fmt.Errorf("message recipients exceed %d", controlplane.MaxRecipients)
		}
		for _, recipient := range p.To {
			if principal, exists := st.Agents[recipient]; !exists || principal.Status != "ACTIVE" {
				return nil, fmt.Errorf("active message recipient %s is required", recipient)
			}
		}
		if len(p.Body) > 1200 {
			return nil, fmt.Errorf("message body exceeds 1200 characters (got %d) — use --body-file for longer content", len(p.Body))
		}
		if p.Kind == "CONTRACT" {
			a, _ := active(st, actor)
			if a.Role != model.RoleOwner && a.Role != model.RoleOrchestrator && !hasApproval(st, "contract:"+id) {
				return nil, errors.New("approved contract publication is required")
			}
		}
	}
	if typ == "message.ack" || typ == "message.reject" || typ == "message.complete" || typ == "message.resolve" {
		m, ok := st.Messages[id]
		if !ok {
			return nil, errors.New("message not found")
		}
		found := false
		recipientStatus := ""
		for _, r := range m.Recipients {
			if r.Principal == actor {
				found = true
				recipientStatus = r.Status
			}
		}
		if !found {
			return nil, errors.New("message recipient required")
		}
		p := payload.(model.MessageResponse)
		switch typ {
		case "message.ack":
			if recipientStatus != "PENDING" {
				return nil, errors.New("pending message obligation is required")
			}
			if m.Kind == "ACTION" || m.Kind == "CONTRACT" {
				p.Response = "ACCEPTED"
			} else {
				p.Response = "ACKNOWLEDGED"
			}
		case "message.reject":
			if recipientStatus != "PENDING" && recipientStatus != "ACCEPTED" {
				return nil, errors.New("open message obligation is required")
			}
			p.Response = "REJECTED"
		case "message.complete":
			if m.Kind != "ACTION" {
				return nil, errors.New("only ACTION messages can complete")
			}
			if recipientStatus != "ACCEPTED" {
				return nil, errors.New("accepted ACTION message is required")
			}
			p.Response = "COMPLETED"
		case "message.resolve":
			if m.Kind != "BLOCKER" {
				return nil, errors.New("only BLOCKER messages can resolve")
			}
			if recipientStatus != "ACKNOWLEDGED" {
				return nil, errors.New("acknowledged BLOCKER message is required")
			}
			p.Response = "RESOLVED"
		}
		payload = p
	}
	if strings.HasPrefix(typ, "invocation.") && typ != "invocation.policy.update" {
		invocation, exists := st.Invocations[id]
		switch p := payload.(type) {
		case model.InvocationRequested:
			if typ != "invocation.request" {
				return nil, errors.New("invalid invocation request transition")
			}
			if id == "" || strings.TrimSpace(p.Target) == "" || strings.TrimSpace(p.Instruction) == "" {
				return nil, errors.New("invocation ID, target, and instruction are required")
			}
			if exists {
				return nil, errors.New("invocation already exists")
			}
			target, targetExists := st.Agents[p.Target]
			if !targetExists || target.Status != "ACTIVE" || target.PrincipalType != model.PrincipalAgent {
				return nil, errors.New("active agent invocation target is required")
			}
			requester, _ := active(st, actor)
			if requester.Role == model.RoleObserver {
				return nil, errors.New("observer cannot request invocations")
			}
			if !actorElevated(st, actor) {
				policy, configured := st.InvocationPolicies[p.Target]
				if !configured || policy.Mode == "MANUAL" {
					if !hasApproval(st, "invocation:"+id) {
						return nil, errors.New("approved invocation is required by target policy")
					}
				} else if policy.Mode == "DISABLED" {
					return nil, errors.New("target invocation policy is disabled")
				} else if policy.Mode == "TRUSTED" && !containsString(policy.TrustedActors, actor) {
					return nil, errors.New("requester is not trusted by target invocation policy")
				}
			}
			if len(p.Instruction)+len(p.ExpectedResult) > controlplane.MaxInvocationBytes {
				return nil, fmt.Errorf("invocation content exceeds %d bytes", controlplane.MaxInvocationBytes)
			}
			priority := strings.ToUpper(strings.TrimSpace(p.Priority))
			if priority == "" {
				priority = "NORMAL"
			}
			if priority != "LOW" && priority != "NORMAL" && priority != "HIGH" && priority != "URGENT" {
				return nil, errors.New("invocation priority must be LOW, NORMAL, HIGH, or URGENT")
			}
			p.Priority = priority
			if p.Deadline != nil {
				if !p.Deadline.After(now) {
					return nil, errors.New("invocation deadline must be in the future")
				}
				if p.Deadline.After(now.Add(controlplane.MaxInvocationTTL)) {
					return nil, fmt.Errorf("invocation deadline exceeds %s", controlplane.MaxInvocationTTL)
				}
			}
			if p.MessageID != "" {
				message, messageExists := st.Messages[p.MessageID]
				if !messageExists {
					return nil, errors.New("related message not found")
				}
				addressed := false
				for _, recipient := range message.To {
					if recipient == p.Target {
						addressed = true
						break
					}
				}
				if !addressed {
					return nil, errors.New("related message is not addressed to the invocation target")
				}
			}
			if p.TaskID != "" {
				if _, taskExists := st.Tasks[p.TaskID]; !taskExists {
					return nil, errors.New("related task not found")
				}
			}
			payload = p
		case model.InvocationNotified:
			if !exists {
				return nil, errors.New("invocation not found")
			}
			if invocation.Status != "PENDING" {
				return nil, fmt.Errorf("cannot notify invocation while %s", invocation.Status)
			}
			if actor != invocation.RequestedBy && actor != invocation.Target && !actorElevated(st, actor) {
				return nil, errors.New("invocation requester, target, owner, or orchestrator required")
			}
			if strings.TrimSpace(p.DeliveryID) == "" || p.Attempt < 1 || p.Attempt > controlplane.MaxDeliveryAttempts {
				return nil, fmt.Errorf("delivery ID and attempt from 1 to %d are required", controlplane.MaxDeliveryAttempts)
			}
			if _, duplicate := st.InvocationDeliveries[p.DeliveryID]; duplicate {
				return nil, errors.New("invocation delivery already exists")
			}
		case model.InvocationClaimed:
			if !exists {
				return nil, errors.New("invocation not found")
			}
			if actor != invocation.Target {
				return nil, errors.New("invocation target required")
			}
			if invocation.Status != "PENDING" && invocation.Status != "NOTIFIED" {
				return nil, fmt.Errorf("invocation is no longer claimable while %s", invocation.Status)
			}
			if invocation.Deadline != nil && !invocation.Deadline.After(now) {
				return nil, errors.New("invocation deadline has passed")
			}
			if strings.TrimSpace(p.RuntimeID) == "" {
				return nil, errors.New("runtime ID is required")
			}
			if p.ClaimUntil.IsZero() {
				p.ClaimUntil = now.Add(controlplane.DefaultClaimLease)
			}
			if !p.ClaimUntil.After(now) || p.ClaimUntil.After(now.Add(controlplane.MaxClaimLease)) {
				return nil, fmt.Errorf("claim lease must be within %s", controlplane.MaxClaimLease)
			}
			payload = p
		case model.InvocationProgress:
			if !exists {
				return nil, errors.New("invocation not found")
			}
			if actor != invocation.Target {
				return nil, errors.New("invocation target required")
			}
			if typ == "invocation.start" && invocation.Status != "CLAIMED" {
				return nil, fmt.Errorf("claimed invocation required, currently %s", invocation.Status)
			}
			if typ == "invocation.resume" && invocation.Status != "WAITING" {
				return nil, fmt.Errorf("waiting invocation required, currently %s", invocation.Status)
			}
		case model.InvocationWaiting:
			if !exists || actor != invocation.Target {
				return nil, errors.New("active invocation target required")
			}
			if invocation.Status != "RUNNING" {
				return nil, fmt.Errorf("running invocation required, currently %s", invocation.Status)
			}
			if strings.TrimSpace(p.Reason) == "" {
				return nil, errors.New("waiting reason is required")
			}
			if p.NextAttemptAt != nil && !p.NextAttemptAt.After(now) {
				return nil, errors.New("next attempt must be in the future")
			}
		case model.InvocationCompleted:
			if !exists || actor != invocation.Target {
				return nil, errors.New("active invocation target required")
			}
			if invocation.Status != "RUNNING" && invocation.Status != "WAITING" {
				return nil, fmt.Errorf("running or waiting invocation required, currently %s", invocation.Status)
			}
			if strings.TrimSpace(p.Summary) == "" {
				return nil, errors.New("completion summary is required")
			}
			if p.ResultMessageID != "" {
				result, resultExists := st.Messages[p.ResultMessageID]
				if !resultExists || result.From != actor {
					return nil, errors.New("result message authored by the invocation target is required")
				}
			}
		case model.InvocationRejected:
			if !exists {
				return nil, errors.New("invocation not found")
			}
			if strings.TrimSpace(p.Reason) == "" {
				return nil, errors.New("rejection or expiry reason is required")
			}
			if typ == "invocation.reject" {
				if actor != invocation.Target || (invocation.Status != "PENDING" && invocation.Status != "NOTIFIED" && invocation.Status != "CLAIMED") {
					return nil, errors.New("open invocation target required")
				}
			} else if typ == "invocation.expire" {
				if actor != invocation.RequestedBy && !actorElevated(st, actor) {
					return nil, errors.New("invocation requester, owner, or orchestrator required")
				}
				if invocation.Deadline == nil || invocation.Deadline.After(now) {
					return nil, errors.New("expired invocation deadline is required")
				}
			}
		case model.InvocationDeliveryFailed:
			if !exists {
				return nil, errors.New("invocation not found")
			}
			if actor != invocation.RequestedBy && actor != invocation.Target && !actorElevated(st, actor) {
				return nil, errors.New("invocation requester, target, owner, or orchestrator required")
			}
			if strings.TrimSpace(p.DeliveryID) == "" || strings.TrimSpace(p.Error) == "" ||
				p.Attempt < 1 || p.Attempt > controlplane.MaxDeliveryAttempts {
				return nil, errors.New("valid delivery ID, attempt, and error are required")
			}
			delivery, deliveryExists := st.InvocationDeliveries[p.DeliveryID]
			if !deliveryExists || delivery.InvocationID != id || delivery.Status != "NOTIFIED" ||
				delivery.Attempt != p.Attempt {
				return nil, errors.New("matching notified delivery is required")
			}
			if p.Final && p.Attempt < controlplane.MaxDeliveryAttempts {
				return nil, fmt.Errorf("delivery cannot dead-letter before attempt %d", controlplane.MaxDeliveryAttempts)
			}
			if !p.Final && (p.NextRetry == nil || !p.NextRetry.After(now)) {
				return nil, errors.New("future retry time is required for a retryable delivery failure")
			}
		default:
			return nil, errors.New("invalid invocation payload")
		}
	}
	if strings.HasPrefix(typ, "runtime.") {
		runtime, exists := st.AgentRuntimes[id]
		switch p := payload.(type) {
		case model.RuntimeRegistered:
			if typ != "runtime.register" {
				return nil, errors.New("invalid runtime registration transition")
			}
			if id == "" || p.AgentID == "" {
				return nil, errors.New("runtime ID and agent ID are required")
			}
			if exists {
				return nil, errors.New("runtime already exists")
			}
			if len(st.AgentRuntimes) >= controlplane.MaxRuntimesPerProject {
				return nil, fmt.Errorf("runtime count exceeds %d", controlplane.MaxRuntimesPerProject)
			}
			target, targetExists := st.Agents[p.AgentID]
			if !targetExists || target.Status != "ACTIVE" || target.PrincipalType != model.PrincipalAgent {
				return nil, errors.New("active agent runtime owner is required")
			}
			if actor != p.AgentID && !actorElevated(st, actor) {
				return nil, errors.New("runtime owner, project owner, or orchestrator required")
			}
			p.Connector = strings.ToUpper(strings.TrimSpace(p.Connector))
			validConnector := p.Connector == "MANUAL" || p.Connector == "MCP" ||
				p.Connector == "LOCAL_PROCESS" || p.Connector == "WEBHOOK" || p.Connector == "QUEUE"
			if !validConnector {
				return nil, errors.New("runtime connector must be MANUAL, MCP, LOCAL_PROCESS, WEBHOOK, or QUEUE")
			}
			if p.MaxConcurrent < 1 || p.MaxConcurrent > controlplane.MaxRuntimeConcurrency {
				return nil, fmt.Errorf("runtime concurrency must be from 1 to %d", controlplane.MaxRuntimeConcurrency)
			}
			if strings.Contains(strings.ToLower(p.ConfigReference), "secret") ||
				strings.Contains(strings.ToLower(p.ConfigReference), "token") ||
				strings.Contains(strings.ToLower(p.ConfigReference), "password") {
				return nil, errors.New("runtime config reference must not contain secret material")
			}
			payload = p
		case model.RuntimeHeartbeat:
			if !exists || actor != runtime.AgentID {
				return nil, errors.New("registered runtime owner required")
			}
			if runtime.Status == "REVOKED" || runtime.Status == "DRAINING" {
				return nil, fmt.Errorf("runtime cannot heartbeat while %s", runtime.Status)
			}
			if !runtime.LastSeenAt.IsZero() && now.Sub(runtime.LastSeenAt) < controlplane.MinHeartbeatInterval {
				return nil, fmt.Errorf("runtime heartbeat interval must be at least %s", controlplane.MinHeartbeatInterval)
			}
			p.Health = strings.ToUpper(strings.TrimSpace(p.Health))
			if p.Health != "HEALTHY" && p.Health != "DEGRADED" {
				return nil, errors.New("runtime health must be HEALTHY or DEGRADED")
			}
			if len(p.ActiveInvocations) > runtime.MaxConcurrent {
				return nil, errors.New("active invocations exceed runtime concurrency")
			}
			seen := map[string]bool{}
			for _, invocationID := range p.ActiveInvocations {
				invocation, invocationExists := st.Invocations[invocationID]
				if !invocationExists || invocation.Target != runtime.AgentID || invocation.RuntimeID != id {
					return nil, fmt.Errorf("active invocation %s is not assigned to this runtime", invocationID)
				}
				if seen[invocationID] {
					return nil, errors.New("active invocation IDs must be unique")
				}
				seen[invocationID] = true
			}
			payload = p
		case model.RuntimeStatusChanged:
			if !exists {
				return nil, errors.New("runtime not found")
			}
			if typ == "runtime.revoke" {
				if !actorElevated(st, actor) {
					return nil, errors.New("owner or orchestrator required to revoke a runtime")
				}
				if runtime.Status == "REVOKED" {
					return nil, errors.New("runtime is already revoked")
				}
			} else {
				if actor != runtime.AgentID && !actorElevated(st, actor) {
					return nil, errors.New("runtime owner, project owner, or orchestrator required")
				}
				if typ == "runtime.drain" && (runtime.Status == "DRAINING" || runtime.Status == "REVOKED") {
					return nil, fmt.Errorf("runtime cannot drain while %s", runtime.Status)
				}
				if typ == "runtime.resume" && runtime.Status != "DRAINING" {
					return nil, errors.New("draining runtime required")
				}
			}
		default:
			return nil, errors.New("invalid runtime payload")
		}
	}
	if typ == "invocation.policy.update" {
		policy, ok := payload.(model.InvocationPolicyUpdated)
		target, exists := st.Agents[id]
		if !ok || !exists || target.Status != "ACTIVE" || target.PrincipalType != model.PrincipalAgent {
			return nil, errors.New("active agent invocation policy target is required")
		}
		if !actorElevated(st, actor) {
			return nil, errors.New("owner or orchestrator required to update invocation policy")
		}
		policy.Mode = strings.ToUpper(strings.TrimSpace(policy.Mode))
		if policy.Mode != "MANUAL" && policy.Mode != "TRUSTED" &&
			policy.Mode != "AUTOMATIC" && policy.Mode != "DISABLED" {
			return nil, errors.New("invocation policy must be MANUAL, TRUSTED, AUTOMATIC, or DISABLED")
		}
		for _, trustedActor := range policy.TrustedActors {
			trusted, trustedExists := st.Agents[trustedActor]
			if !trustedExists || trusted.Status != "ACTIVE" {
				return nil, fmt.Errorf("trusted actor %s is not active", trustedActor)
			}
		}
		payload = policy
	}
	if strings.HasPrefix(typ, "document.") {
		p := payload.(model.DocumentPayload)
		switch typ {
		case "document.create":
			if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Body) == "" {
				return nil, errors.New("title and body are required")
			}
			if _, exists := st.Documents[id]; exists {
				return nil, errors.New("document already exists")
			}
		case "document.update", "document.supersede":
			if _, exists := st.Documents[id]; !exists {
				return nil, errors.New("document not found")
			}
			if typ == "document.update" && (strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Body) == "") {
				return nil, errors.New("title and body are required")
			}
			if typ == "document.supersede" {
				if p.ReplacementID == "" || p.ReplacementID == id {
					return nil, errors.New("a different replacement document is required")
				}
				if _, exists := st.Documents[p.ReplacementID]; !exists {
					return nil, errors.New("replacement document not found")
				}
			}
		}
	}
	if typ == "approval.request" {
		request, ok := payload.(model.ApprovalRequested)
		if !ok || (request.Tier != "ORCHESTRATOR" && request.Tier != "HUMAN") ||
			strings.TrimSpace(request.Action) == "" {
			return nil, errors.New("approval tier and action are required")
		}
		if _, exists := st.Approvals[id]; exists {
			return nil, errors.New("approval already exists")
		}
	}
	if typ == "approval.approve" || typ == "approval.reject" {
		approval, exists := st.Approvals[id]
		if !exists || approval.Status != "PENDING" {
			return nil, errors.New("pending approval is required")
		}
	}
	if typ == "decision.create" {
		decision, ok := payload.(model.DecisionPayload)
		if !ok || strings.TrimSpace(decision.Title) == "" || strings.TrimSpace(decision.Statement) == "" {
			return nil, errors.New("decision title and statement are required")
		}
		if _, exists := st.Decisions[id]; exists {
			return nil, errors.New("decision already exists")
		}
	}
	if strings.HasPrefix(typ, "env.") {
		switch typ {
		case "env.set":
			p := payload.(model.EnvSetPayload)
			if strings.TrimSpace(p.Key) == "" {
				return nil, errors.New("key is required")
			}
		case "env.delete":
			p := payload.(model.EnvDeletePayload)
			if p.Key == "" {
				return nil, errors.New("key is required")
			}
		}
	}
	return payload, nil
}

func actorElevated(state model.State, actor string) bool {
	principal, ok := state.Agents[actor]
	return ok && principal.Status == "ACTIVE" &&
		(principal.Role == model.RoleOwner || principal.Role == model.RoleOrchestrator)
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func migrationSeedEvent(typ string) bool {
	switch typ {
	case "agent.register", "agent.activate", "task.create", "task.claim", "task.start", "task.block", "decision.create", "message.post", "document.create":
		return true
	default:
		return false
	}
}
func (s *Service) Register(actor, display string, pt model.PrincipalType) (model.Event, error) {
	cfg, e := s.Store.Config()
	if e != nil {
		return model.Event{}, e
	}
	cred, e := identity.Generate(cfg.ProjectID, actor)
	if e != nil {
		return model.Event{}, e
	}
	if e = s.Store.Credentials.Put(cred); e != nil {
		return model.Event{}, e
	}
	user, _ := identity.LoadUserConfig()
	name := cfg.ProjectID + ":" + actor
	user.Profiles[name] = identity.Profile{Name: name, ProjectID: cfg.ProjectID, Actor: actor, ProjectRoot: s.Store.Root}
	if user.ActiveProfile == "" {
		user.ActiveProfile = name
	}
	if e = identity.SaveUserConfig(user); e != nil {
		return model.Event{}, e
	}
	payload := model.AgentRegistered{PublicKey: cred.PublicKey, PrincipalType: pt, DisplayName: display}
	if s.remote != nil {
		event, remoteErr := s.executeRemote(actor, "agent.register", actor, payload)
		if remoteErr != nil {
			_ = s.Store.Credentials.Delete(cfg.ProjectID, actor)
			delete(user.Profiles, name)
			_ = identity.SaveUserConfig(user)
			return model.Event{}, remoteErr
		}
		return event, nil
	}
	return s.Store.Append(actor, "agent.register", actor, payload)
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
	var ev model.Event
	if s.remote != nil {
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
		ev = model.Event{
			SchemaVersion: model.SchemaVersion, PayloadVersion: 1, ID: remoteEvent.ID,
			Sequence: remoteEvent.Sequence, Time: remoteEvent.Time, Actor: remoteEvent.Actor,
			Type: remoteEvent.Type, EntityID: remoteEvent.EntityID, Data: remoteEvent.Payload,
			PreviousHash: remoteEvent.PreviousHash, Hash: remoteEvent.Hash, Signature: command.Signature,
			KeyFingerprint: identity.Fingerprint(old.PublicKey), ServerReceipt: metadata.Receipt,
			Consistency: metadata.Consistency, Connectivity: metadata.Connectivity,
		}
	} else {
		ev, e = s.Store.AppendWithCredential(actor, "agent.rotate-key", actor, payload, old)
	}
	if e != nil {
		return model.Event{}, e
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
	storage := "git"
	if n > cfg.ArtifactLimitBytes {
		if _, e = os.Stat(filepath.Join(s.Store.Root, store.Runtime, ".gitattributes")); e != nil {
			return model.Event{}, errors.New("artifact exceeds 5 MiB; configure Git LFS first")
		}
		storage = "git-lfs"
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
	return s.Store.Append(actor, "artifact.add", sum, model.ArtifactAdded{SHA256: sum, Size: n, Name: filepath.Base(path), MediaType: mt, Storage: storage})
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

func ProjectionHash(state model.State) string {
	state.Integrity = model.Integrity{}
	raw, _ := json.Marshal(state)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
