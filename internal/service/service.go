package service

import (
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

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

type Service struct{ Store *store.Store }

func New(root string) *Service { return &Service{Store: store.Open(root)} }
func (s *Service) State() (model.State, error) {
	ev, e := s.Store.Events()
	if e != nil {
		return model.State{}, e
	}
	st := model.State{Agents: map[string]model.Agent{}, Tasks: map[string]model.Task{}, Messages: map[string]model.Message{}, Approvals: map[string]model.Approval{}, Decisions: map[string]model.Decision{}, Documents: map[string]model.Document{}, Sessions: map[string]model.SessionPayload{}, Artifacts: map[string]model.Artifact{}}
	unknown := 0
	for _, v := range ev {
		if !model.KnownEventType(v.Type) {
			unknown++
			continue
		}
		if e = apply(&st, v); e != nil {
			return st, fmt.Errorf("project event %s: %w", v.ID, e)
		}
	}
	st.Integrity = model.Integrity{Verified: s.Store.Verify() == nil, EventCount: len(ev), Head: s.Store.Head(), UnknownEvents: unknown, Remote: s.Store.Remote(), SyncState: "local-only"}
	if st.Integrity.Remote != "" {
		st.Integrity.SyncState = "configured"
	}
	return st, nil
}
func apply(s *model.State, e model.Event) error {
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
	if incomplete, status := s.Store.MigrationIncomplete(); incomplete && !migrationSeedEvent(typ) {
		return model.Event{}, fmt.Errorf("runtime migration is incomplete (%s); event %s is blocked", status, typ)
	}
	if incomplete, state := s.Store.CutoverIncomplete(); incomplete && !migrationSeedEvent(typ) {
		return model.Event{}, fmt.Errorf("migration cutover is incomplete (%s); event %s is blocked", state, typ)
	} else if !incomplete && state == store.CutoverActivated && !s.Store.ManagedBootstrapValid() {
		return model.Event{}, errors.New("split-brain cutover detected; durable work is blocked")
	}
	st, e := s.State()
	if e != nil {
		return model.Event{}, e
	}
	if actor == "" {
		return model.Event{}, errors.New("actor is required")
	}
	if typ != "agent.register" {
		a, x := active(st, actor)
		if x != nil {
			return model.Event{}, x
		}
		if elevated(typ) && a.Role != model.RoleOwner && a.Role != model.RoleOrchestrator {
			return model.Event{}, errors.New("owner or orchestrator role required")
		}
		if typ == "approval.approve" {
			ap := st.Approvals[id]
			if ap.Tier == "HUMAN" && a.PrincipalType != model.PrincipalHuman {
				return model.Event{}, errors.New("human principal required for this approval")
			}
		}
	}
	if typ == "agent.activate" {
		if _, ok := st.Agents[id]; !ok {
			return model.Event{}, errors.New("pending principal not found")
		}
	}
	if strings.HasPrefix(typ, "task.") {
		t, exists := st.Tasks[id]
		if typ != "task.create" && !exists {
			return model.Event{}, errors.New("task not found")
		}
		switch p := payload.(type) {
		case model.TaskCreated:
			if p.Title == "" || p.Repository == "" || p.Branch == "" || len(p.Resources) == 0 {
				return model.Event{}, errors.New("title, repository, branch, and resources are required")
			}
		case model.TaskClaimed:
			a, _ := active(st, actor)
			if a.Role == model.RoleObserver {
				return model.Event{}, errors.New("observer cannot claim tasks")
			}
			if !scopeAllows(a.Scopes, t.Resources) {
				return model.Event{}, errors.New("task resources exceed principal scopes")
			}
			for _, v := range st.Tasks {
				if v.ID != id && v.Owner != "" && !v.Archived && v.Status != "COMPLETED" && v.Status != "CANCELLED" && overlap(t.Resources, v.Resources) {
					if !hasApproval(st, "shared-write:"+id+":"+v.ID) && !hasApproval(st, "shared-write:"+v.ID+":"+id) {
						return model.Event{}, fmt.Errorf("write lease overlaps task %s", v.ID)
					}
				}
			}
			p.LeaseUntil = time.Now().UTC().Add(4 * time.Hour)
			payload = p
		case model.TaskRenewed:
			if t.Owner != actor {
				return model.Event{}, errors.New("task owner required")
			}
			if strings.TrimSpace(p.Progress) == "" {
				return model.Event{}, errors.New("progress summary is required")
			}
			p.LeaseUntil = time.Now().UTC().Add(4 * time.Hour)
			payload = p
		case model.TaskHandoff:
			if t.Owner != actor {
				return model.Event{}, errors.New("task owner required")
			}
		case model.TaskStatus:
			if typ == "task.takeover" && !hasApproval(st, "task.takeover:"+id) {
				return model.Event{}, errors.New("approved takeover is required")
			}
			if typ == "task.complete" && t.Risk != "ROUTINE" {
				if t.Status != "REVIEW" {
					return model.Event{}, errors.New("elevated task requires review before completion")
				}
				a, _ := active(st, actor)
				if a.Role != model.RoleOwner && a.Role != model.RoleOrchestrator {
					return model.Event{}, errors.New("eligible reviewer required")
				}
			}
			if typ == "task.handoff.accept" && t.HandoffTo != actor {
				return model.Event{}, errors.New("handoff target required")
			}
			if typ != "task.handoff.accept" && typ != "task.takeover" && t.Owner != "" && t.Owner != actor {
				a, _ := active(st, actor)
				if a.Role != model.RoleOwner && a.Role != model.RoleOrchestrator {
					return model.Event{}, errors.New("task owner or orchestrator required")
				}
			}
		}
	}
	if typ == "message.post" {
		p := payload.(model.MessagePosted)
		valid := map[string]bool{"FYI": true, "ACTION": true, "CONTRACT": true, "BLOCKER": true, "DECISION": true}
		if !valid[p.Kind] || len(p.To) == 0 || p.Subject == "" {
			return model.Event{}, errors.New("valid kind, recipient, and subject are required")
		}
		if len(p.Body) > 1200 {
			return model.Event{}, errors.New("message body exceeds 1200 characters")
		}
		if p.Kind == "CONTRACT" {
			a, _ := active(st, actor)
			if a.Role != model.RoleOwner && a.Role != model.RoleOrchestrator && !hasApproval(st, "contract:"+id) {
				return model.Event{}, errors.New("approved contract publication is required")
			}
		}
	}
	if typ == "message.ack" || typ == "message.reject" || typ == "message.complete" || typ == "message.resolve" {
		m, ok := st.Messages[id]
		if !ok {
			return model.Event{}, errors.New("message not found")
		}
		found := false
		for _, r := range m.Recipients {
			if r.Principal == actor {
				found = true
			}
		}
		if !found {
			return model.Event{}, errors.New("message recipient required")
		}
		p := payload.(model.MessageResponse)
		switch typ {
		case "message.ack":
			if m.Kind == "ACTION" || m.Kind == "CONTRACT" {
				p.Response = "ACCEPTED"
			} else {
				p.Response = "ACKNOWLEDGED"
			}
		case "message.reject":
			p.Response = "REJECTED"
		case "message.complete":
			if m.Kind != "ACTION" {
				return model.Event{}, errors.New("only ACTION messages can complete")
			}
			p.Response = "COMPLETED"
		case "message.resolve":
			if m.Kind != "BLOCKER" {
				return model.Event{}, errors.New("only BLOCKER messages can resolve")
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
				return model.Event{}, errors.New("title and body are required")
			}
			if _, exists := st.Documents[id]; exists {
				return model.Event{}, errors.New("document already exists")
			}
		case "document.update", "document.supersede":
			if _, exists := st.Documents[id]; !exists {
				return model.Event{}, errors.New("document not found")
			}
			if typ == "document.update" && (strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Body) == "") {
				return model.Event{}, errors.New("title and body are required")
			}
			if typ == "document.supersede" {
				if p.ReplacementID == "" || p.ReplacementID == id {
					return model.Event{}, errors.New("a different replacement document is required")
				}
				if _, exists := st.Documents[p.ReplacementID]; !exists {
					return model.Event{}, errors.New("replacement document not found")
				}
			}
		}
	}
	return s.Store.Append(actor, typ, id, payload)
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
	return s.Store.Append(actor, "agent.register", actor, model.AgentRegistered{PublicKey: cred.PublicKey, PrincipalType: pt, DisplayName: display})
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
	ev, e := s.Store.AppendWithCredential(actor, "agent.rotate-key", actor, model.AgentKeyRotated{PublicKey: next.PublicKey, PreviousFingerprint: identity.Fingerprint(old.PublicKey)}, old)
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
	if e := s.Store.Verify(); e != nil {
		return e
	}
	ev, e := s.Store.Events()
	if e != nil {
		return e
	}
	enc := json.NewEncoder(w)
	for _, v := range ev {
		if e = enc.Encode(v); e != nil {
			return e
		}
	}
	return nil
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
