package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

type Service struct{ Store *store.Store }

func New(root string) *Service { return &Service{Store: store.Open(root)} }
func (s *Service) State() (model.State, error) {
	e, err := s.Store.Events()
	if err != nil {
		return model.State{}, err
	}
	st := model.State{Agents: map[string]model.Agent{}, Tasks: map[string]model.Task{}, Messages: map[string]model.Message{}, Approvals: map[string]model.Approval{}, Decisions: map[string]map[string]any{}, Sessions: map[string]map[string]any{}}
	for _, v := range e {
		apply(&st, v)
	}
	return st, nil
}
func str(d map[string]any, k string) string { v, _ := d[k].(string); return v }
func list(d map[string]any, k string) []string {
	if one, ok := d[k].(string); ok && one != "" {
		return []string{one}
	}
	a, _ := d[k].([]any)
	o := []string{}
	for _, v := range a {
		if x, ok := v.(string); ok {
			o = append(o, x)
		}
	}
	if x, ok := d[k].([]string); ok {
		return x
	}
	return o
}
func apply(s *model.State, e model.Event) {
	d := e.Data
	switch e.Type {
	case "agent.register":
		s.Agents[e.EntityID] = model.Agent{ID: e.EntityID, Status: "PENDING"}
	case "agent.activate":
		a := s.Agents[e.EntityID]
		a.Status = "ACTIVE"
		a.Capabilities = list(d, "capabilities")
		a.Scopes = list(d, "scopes")
		s.Agents[e.EntityID] = a
	case "agent.suspend":
		a := s.Agents[e.EntityID]
		a.Status = "SUSPENDED"
		s.Agents[e.EntityID] = a
	case "task.create":
		s.Tasks[e.EntityID] = model.Task{ID: e.EntityID, Title: str(d, "title"), Status: "OPEN", Repository: str(d, "repository"), Branch: str(d, "branch"), Resources: list(d, "resources")}
	case "task.claim":
		t := s.Tasks[e.EntityID]
		t.Owner = e.Actor
		t.Status = "CLAIMED"
		t.LeaseUntil, _ = time.Parse(time.RFC3339Nano, str(d, "lease_until"))
		t.StaleUntil = t.LeaseUntil.Add(time.Hour)
		s.Tasks[e.EntityID] = t
	case "task.start":
		t := s.Tasks[e.EntityID]
		t.Status = "IN_PROGRESS"
		s.Tasks[e.EntityID] = t
	case "task.renew":
		t := s.Tasks[e.EntityID]
		t.LeaseUntil, _ = time.Parse(time.RFC3339Nano, str(d, "lease_until"))
		t.StaleUntil = t.LeaseUntil.Add(time.Hour)
		s.Tasks[e.EntityID] = t
	case "task.block":
		t := s.Tasks[e.EntityID]
		t.Status = "BLOCKED"
		s.Tasks[e.EntityID] = t
	case "task.review":
		t := s.Tasks[e.EntityID]
		t.Status = "REVIEW"
		s.Tasks[e.EntityID] = t
	case "task.complete":
		t := s.Tasks[e.EntityID]
		t.Status = "COMPLETED"
		s.Tasks[e.EntityID] = t
	case "task.cancel":
		t := s.Tasks[e.EntityID]
		t.Status = "CANCELLED"
		s.Tasks[e.EntityID] = t
	case "task.handoff":
		t := s.Tasks[e.EntityID]
		t.HandoffTo = str(d, "to")
		s.Tasks[e.EntityID] = t
	case "task.handoff.accept":
		t := s.Tasks[e.EntityID]
		t.Owner = e.Actor
		t.HandoffTo = ""
		s.Tasks[e.EntityID] = t
	case "message.post":
		s.Messages[e.EntityID] = model.Message{ID: e.EntityID, Kind: str(d, "kind"), From: e.Actor, To: list(d, "to"), Subject: str(d, "subject"), Body: str(d, "body"), Status: "OPEN"}
	case "message.ack":
		m := s.Messages[e.EntityID]
		m.Status = "ACKNOWLEDGED"
		s.Messages[e.EntityID] = m
	case "message.reject":
		m := s.Messages[e.EntityID]
		m.Status = "REJECTED"
		s.Messages[e.EntityID] = m
	case "approval.request":
		s.Approvals[e.EntityID] = model.Approval{ID: e.EntityID, Kind: str(d, "kind"), Status: "PENDING", Requester: e.Actor, Affected: list(d, "affected")}
	case "approval.approve":
		a := s.Approvals[e.EntityID]
		a.Status = "APPROVED"
		s.Approvals[e.EntityID] = a
	case "approval.reject":
		a := s.Approvals[e.EntityID]
		a.Status = "REJECTED"
		s.Approvals[e.EntityID] = a
	case "decision.create", "decision.supersede":
		s.Decisions[e.EntityID] = d
	case "session.start":
		s.Sessions[e.EntityID] = d
	case "session.end":
		delete(s.Sessions, e.EntityID)
	}
}
func overlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y || strings.HasPrefix(x, y+"/") || strings.HasPrefix(y, x+"/") {
				return true
			}
		}
	}
	return false
}
func (s *Service) Execute(actor, typ, id string, d map[string]any) (model.Event, error) {
	st, err := s.State()
	if err != nil {
		return model.Event{}, err
	}
	if actor == "" {
		actor = "owner"
	}
	if len(str(d, "summary")) > 1200 {
		return model.Event{}, errors.New("summary exceeds 1200 characters")
	}
	if strings.HasPrefix(typ, "agent.") && typ != "agent.register" && actor != "owner" {
		return model.Event{}, errors.New("owner or orchestrator authorization required")
	}
	if typ == "task.claim" {
		a, ok := st.Agents[actor]
		if !ok || a.Status != "ACTIVE" {
			return model.Event{}, errors.New("active agent required")
		}
		t, ok := st.Tasks[id]
		if !ok {
			return model.Event{}, errors.New("task not found")
		}
		for _, v := range st.Tasks {
			if v.ID != id && v.Owner != "" && v.Status != "COMPLETED" && v.Status != "CANCELLED" && overlap(t.Resources, v.Resources) {
				return model.Event{}, fmt.Errorf("write lease overlaps task %s", v.ID)
			}
		}
		d["lease_until"] = time.Now().UTC().Add(4 * time.Hour).Format(time.RFC3339Nano)
	}
	if typ == "task.renew" {
		d["lease_until"] = time.Now().UTC().Add(4 * time.Hour).Format(time.RFC3339Nano)
	}
	if strings.HasPrefix(typ, "task.") && typ != "task.create" && typ != "task.claim" && typ != "task.takeover" && typ != "task.handoff.accept" {
		t, ok := st.Tasks[id]
		if !ok {
			return model.Event{}, errors.New("task not found")
		}
		if t.Owner != "" && t.Owner != actor && actor != "owner" {
			return model.Event{}, errors.New("task owner authorization required")
		}
	}
	if typ == "task.takeover" {
		return model.Event{}, errors.New("approved takeover required; request approval first")
	}
	if typ == "task.handoff.accept" {
		t := st.Tasks[id]
		if t.HandoffTo != actor {
			return model.Event{}, errors.New("handoff target required")
		}
	}
	if typ == "message.post" {
		k := str(d, "kind")
		valid := map[string]bool{"FYI": true, "ACTION": true, "CONTRACT": true, "BLOCKER": true, "DECISION": true}
		if !valid[k] {
			return model.Event{}, errors.New("invalid message kind")
		}
	}
	return s.Store.Append(actor, typ, id, d)
}
func (s *Service) AddArtifact(actor, path string) (model.Event, error) {
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
	if n > 5*1024*1024 {
		return model.Event{}, errors.New("artifact exceeds 5 MiB; configure Git LFS or external storage")
	}
	sum := hex.EncodeToString(h.Sum(nil))
	dst := filepath.Join(s.Store.Root, store.Runtime, "artifacts", "sha256", sum)
	if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
		return model.Event{}, e
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return model.Event{}, e
	}
	if e = os.WriteFile(dst, b, 0600); e != nil {
		return model.Event{}, e
	}
	return s.Store.Append(actor, "artifact.add", sum, map[string]any{"sha256": sum, "size": n, "name": filepath.Base(path)})
}
func SortedKeys[V any](m map[string]V) []string {
	o := make([]string, 0, len(m))
	for k := range m {
		o = append(o, k)
	}
	sort.Strings(o)
	return o
}
