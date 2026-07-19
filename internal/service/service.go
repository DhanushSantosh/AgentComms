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
	st := model.State{Agents: map[string]model.Agent{}, Tasks: map[string]model.Task{}, Messages: map[string]model.Message{}, Approvals: map[string]model.Approval{}, Decisions: map[string]model.Decision{}, Documents: map[string]model.Document{}, Env: map[string]model.EnvEntry{}, Sessions: map[string]model.SessionPayload{}, Artifacts: map[string]model.Artifact{}}
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
