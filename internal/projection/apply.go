package projection

import (
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
)

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
		if p.Role == model.RoleOrchestrator {
			consumeOrchestratorGrantApproval(s, e.EntityID)
		}
	case *model.AgentRoleSwitched:
		// Only Role changes -- Capabilities and Scopes are untouched,
		// unlike AgentActivated. See RFC 0018.
		a := s.Agents[e.EntityID]
		a.Role = p.Role
		s.Agents[e.EntityID] = a
		if p.Role == model.RoleOrchestrator {
			// e.EntityID == e.Actor here always -- agent.switch-role is
			// self-service only (ValidateTransition rejects id != actor).
			consumeOrchestratorGrantApproval(s, e.EntityID)
		}
	case *model.AgentKeyRotated:
		a := s.Agents[e.EntityID]
		a.PublicKey = p.PublicKey
		a.KeyFingerprint = identity.Fingerprint(p.PublicKey)
		s.Agents[e.EntityID] = a
	case *model.AgentElevatedKeyRegistered:
		a := s.Agents[e.EntityID]
		a.ElevatedPublicKey = p.PublicKey
		a.ElevatedKeyFingerprint = identity.Fingerprint(p.PublicKey)
		s.Agents[e.EntityID] = a
	case *model.AgentRenamed:
		a := s.Agents[e.EntityID]
		a.DisplayName = p.DisplayName
		s.Agents[e.EntityID] = a
	case *model.AgentDeleted:
		delete(s.Agents, e.EntityID)
	case *model.TaskCreated:
		s.Tasks[e.EntityID] = model.Task{ID: e.EntityID, Title: p.Title, Summary: p.Summary, Status: "OPEN", Repository: p.Repository, Branch: p.Branch, Worktree: p.Worktree, Resources: p.Resources, ExternalRef: p.ExternalRef, Risk: defaultRisk(p.Risk)}
	case *model.TaskOffered:
		t := s.Tasks[e.EntityID]
		t.Status = "OFFERED"
		t.Offers = append(t.Offers, model.Offer{ID: e.ID, To: p.To, ExpiresAt: p.ExpiresAt, Status: "PENDING"})
		s.Tasks[e.EntityID] = t
	case *model.TaskClaimed:
		settings := model.EffectiveProjectSettings(s.ProjectSettings)
		staleGrace, _ := time.ParseDuration(settings.StaleGrace)
		t := s.Tasks[e.EntityID]
		t.Owner = e.Actor
		t.Status = "CLAIMED"
		t.LeaseUntil = p.LeaseUntil
		t.StaleUntil = p.LeaseUntil.Add(staleGrace)
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
		settings := model.EffectiveProjectSettings(s.ProjectSettings)
		staleGrace, _ := time.ParseDuration(settings.StaleGrace)
		t := s.Tasks[e.EntityID]
		t.LeaseUntil = p.LeaseUntil
		t.StaleUntil = p.LeaseUntil.Add(staleGrace)
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
			settings := model.EffectiveProjectSettings(s.ProjectSettings)
			defaultLease, _ := time.ParseDuration(settings.DefaultLease)
			staleGrace, _ := time.ParseDuration(settings.StaleGrace)
			t.Owner = e.Actor
			t.Status = "CLAIMED"
			t.LeaseUntil = e.Time.Add(defaultLease)
			t.StaleUntil = t.LeaseUntil.Add(staleGrace)
			consumeApprovedAction(s, "task.takeover:"+e.EntityID)
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
			Scopes: p.Scopes, Priority: priority, ConsumerMode: effectiveConsumerMode(p.ConsumerMode),
			PreferredRuntimeID: p.PreferredRuntimeID, Status: "PENDING",
			CreatedAt: e.Time, Deadline: p.Deadline,
		}
	case *model.InvocationDeliveryAttempted:
		now := e.Time
		attemptUntil := p.AttemptUntil
		s.InvocationDeliveries[p.DeliveryID] = model.InvocationDelivery{
			ID: p.DeliveryID, InvocationID: e.EntityID, RuntimeID: p.RuntimeID,
			Transport: p.Transport, HostID: p.HostID, EndpointID: p.EndpointID, Attempt: p.Attempt,
			Manual: p.Manual, Status: "ATTEMPTED", AttemptedAt: &now,
			AttemptUntil: &attemptUntil,
		}
	case *model.InvocationNotified:
		invocation := s.Invocations[e.EntityID]
		now := e.Time
		if invocation.Status == "PENDING" {
			invocation.Status = "NOTIFIED"
		}
		s.Invocations[e.EntityID] = invocation
		delivery, attempted := s.InvocationDeliveries[p.DeliveryID]
		if !attempted {
			// Compatibility for signed 2.0 histories where notify itself
			// created the delivery record.
			delivery = model.InvocationDelivery{
				ID: p.DeliveryID, InvocationID: e.EntityID,
				RuntimeID: p.RuntimeID, Attempt: p.Attempt, Status: "NOTIFIED",
			}
		} else {
			delivery.Status = "SUCCEEDED"
		}
		delivery.RuntimeID = p.RuntimeID
		delivery.Transport = p.Transport
		delivery.EndpointID = p.EndpointID
		delivery.Evidence = p.Evidence
		delivery.NotifiedAt = &now
		s.InvocationDeliveries[p.DeliveryID] = delivery
	case *model.InvocationClaimed:
		invocation := s.Invocations[e.EntityID]
		claimUntil := p.ClaimUntil
		claimedAt := e.Time
		invocation.Status = "CLAIMED"
		invocation.ClaimedBy = e.Actor
		invocation.RuntimeID = p.RuntimeID
		invocation.ClaimedAt = &claimedAt
		invocation.ClaimUntil = &claimUntil
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
		} else if e.Type == "invocation.cancel" {
			invocation.Status = "CANCELLED"
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
			status = "EXHAUSTED"
		}
		if invocation.Status == "PENDING" || invocation.Status == "NOTIFIED" {
			if hasSuccessfulDelivery(*s, e.EntityID) {
				invocation.Status = "NOTIFIED"
			} else {
				invocation.Status = "PENDING"
			}
		}
		s.Invocations[e.EntityID] = invocation
		delivery := s.InvocationDeliveries[p.DeliveryID]
		delivery.Status = status
		delivery.FailedAt = &now
		delivery.NextRetryAt = p.NextRetry
		delivery.Error = p.Error
		s.InvocationDeliveries[p.DeliveryID] = delivery
	case *model.RuntimeRegistered:
		kind := effectiveRuntimeKind(p.Kind, p.Connector)
		s.AgentRuntimes[e.EntityID] = model.AgentRuntime{
			ID: e.EntityID, AgentID: p.AgentID, Kind: kind,
			Connector: p.Connector, ConfigReference: p.ConfigReference, HostID: p.HostID,
			Status: "OFFLINE", Health: "UNKNOWN",
			MaxConcurrent: p.MaxConcurrent, Scopes: p.Scopes, Capabilities: p.Capabilities,
			RegisteredAt: e.Time, LastChangedBy: e.Actor,
		}
		if _, configured := s.InvocationPolicies[p.AgentID]; !configured {
			if kind == model.RuntimeKindInteractive || p.Connector == "INTERACTIVE" {
				s.InvocationPolicies[p.AgentID] = model.InvocationPolicy{
					AgentID: p.AgentID, Mode: "AUTOMATIC",
					DefaultConsumerMode: model.ConsumerModeEither,
					AllowedConsumerModes: []model.ConsumerMode{
						model.ConsumerModeInteractiveOnly,
						model.ConsumerModeWorkerOnly,
						model.ConsumerModeEither,
					},
					RequireHumanForSensitive: true,
					UpdatedBy:                e.Actor, UpdatedAt: e.Time,
				}
			}
		}
	case *model.RuntimeConfigured:
		runtime := s.AgentRuntimes[e.EntityID]
		runtime.Kind = effectiveRuntimeKind(p.Kind, p.Connector)
		runtime.Connector = p.Connector
		runtime.ConfigReference = p.ConfigReference
		runtime.HostID = p.HostID
		runtime.EndpointID = ""
		runtime.MaxConcurrent = p.MaxConcurrent
		runtime.Scopes = p.Scopes
		runtime.Capabilities = p.Capabilities
		runtime.Status = "OFFLINE"
		runtime.Health = "UNKNOWN"
		runtime.Reason = ""
		runtime.LastChangedBy = e.Actor
		s.AgentRuntimes[e.EntityID] = runtime
	case *model.RuntimeHeartbeat:
		runtime := s.AgentRuntimes[e.EntityID]
		runtime.Status = "ONLINE"
		runtime.Health = p.Health
		runtime.EndpointID = p.EndpointID
		runtime.ActiveInvocations = p.ActiveInvocations
		runtime.LastSeenAt = e.Time
		runtime.LastChangedBy = e.Actor
		runtime.Reason = ""
		s.AgentRuntimes[e.EntityID] = runtime
	case *model.RuntimeStatusChanged:
		if e.Type == "agent.revoke" {
			a := s.Agents[e.EntityID]
			a.Status = "REVOKED"
			s.Agents[e.EntityID] = a
			// Cascade revocation to the principal's runtimes so neither the
			// delivery coordinator nor a consumer can route new work to them.
			for rid, rt := range s.AgentRuntimes {
				if rt.AgentID == e.EntityID && rt.Status != "REVOKED" {
					rt.Status, rt.Reason, rt.LastChangedBy = "REVOKED", "agent revoked", e.Actor
					s.AgentRuntimes[rid] = rt
				}
			}
			break
		}
		runtime := s.AgentRuntimes[e.EntityID]
		switch e.Type {
		case "runtime.offline":
			runtime.Status = "OFFLINE"
			runtime.Health = "UNKNOWN"
			runtime.EndpointID = ""
			runtime.ActiveInvocations = nil
		case "runtime.drain":
			runtime.Status = "DRAINING"
		case "runtime.resume":
			runtime.Status = "OFFLINE"
		case "runtime.revoke":
			runtime.Status = "REVOKED"
		case "runtime.delete":
			delete(s.AgentRuntimes, e.EntityID)
			return nil
		}
		runtime.Reason = p.Reason
		runtime.LastChangedBy = e.Actor
		s.AgentRuntimes[e.EntityID] = runtime
	case *model.InvocationPolicyUpdated:
		defaultConsumer := effectiveConsumerMode(p.DefaultConsumerMode)
		allowedConsumers := p.AllowedConsumerModes
		if len(allowedConsumers) == 0 {
			allowedConsumers = []model.ConsumerMode{
				model.ConsumerModeInteractiveOnly,
				model.ConsumerModeWorkerOnly,
				model.ConsumerModeEither,
			}
		}
		s.InvocationPolicies[e.EntityID] = model.InvocationPolicy{
			AgentID: e.EntityID, Mode: p.Mode, TrustedActors: p.TrustedActors,
			AllowedScopes: p.AllowedScopes, DefaultConsumerMode: defaultConsumer,
			AllowedConsumerModes:          allowedConsumers,
			PreferredInteractiveRuntimeID: p.PreferredInteractiveRuntimeID,
			RequireHumanForSensitive:      p.RequireHumanForSensitive,
			UpdatedBy:                     e.Actor, UpdatedAt: e.Time,
		}
	case *model.ProjectSettingsUpdated:
		s.ProjectSettings = model.ProjectSettings{
			DefaultLease: p.DefaultLease, StaleGrace: p.StaleGrace,
			ActiveRetention: p.ActiveRetention, SummaryLimit: p.SummaryLimit,
			ArtifactLimitBytes: p.ArtifactLimitBytes, RequireReview: p.RequireReview,
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

// consumeOrchestratorGrantApproval marks the specific HUMAN-tier approval
// that authorized principalID's ORCHESTRATOR grant as CONSUMED, so it can
// never satisfy protocol.hasOrchestratorGrantApproval again -- the next
// attempt to grant that principal ORCHESTRATOR, whenever it happens,
// requires a brand new request-then-separately-approved approval. See RFC
// 0023. Mirrors that function's exact lookup (same conventional ID,
// derived the same way from principalID); ValidateTransition already
// required this approval to exist and be APPROVED for the AgentActivated/
// AgentRoleSwitched event applied here to have been produced at all, so it
// is guaranteed present.
func consumeOrchestratorGrantApproval(s *model.State, principalID string) {
	approvalID := protocol.OrchestratorGrantApprovalID(principalID)
	approval, exists := s.Approvals[approvalID]
	if !exists {
		return
	}
	approval.Status = "CONSUMED"
	s.Approvals[approvalID] = approval
}

// consumeApprovedAction spends one approval that authorized a single event.
// Sorting makes projection deterministic when callers have independently
// requested more than one approval for the same action: each approved record
// can authorize one event, while one takeover approval can never authorize a
// later takeover of the same long-lived task ID.
func consumeApprovedAction(s *model.State, action string) {
	ids := make([]string, 0)
	for id, approval := range s.Approvals {
		if approval.Action == action && approval.Status == "APPROVED" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	sort.Strings(ids)
	approval := s.Approvals[ids[0]]
	approval.Status = "CONSUMED"
	s.Approvals[ids[0]] = approval
}
func defaultRisk(v string) string {
	if v == "" {
		return "ROUTINE"
	}
	return strings.ToUpper(v)
}
func effectiveRuntimeKind(kind model.RuntimeKind, connector string) model.RuntimeKind {
	if kind != "" {
		return kind
	}
	if strings.EqualFold(connector, "INTERACTIVE") {
		return model.RuntimeKindInteractive
	}
	return model.RuntimeKindWorker
}
func effectiveConsumerMode(mode model.ConsumerMode) model.ConsumerMode {
	switch mode {
	case model.ConsumerModeInteractiveOnly, model.ConsumerModeWorkerOnly, model.ConsumerModeEither:
		return mode
	default:
		return model.ConsumerModeEither
	}
}
func hasSuccessfulDelivery(state model.State, invocationID string) bool {
	for _, delivery := range state.InvocationDeliveries {
		if delivery.InvocationID == invocationID &&
			(delivery.Status == "SUCCEEDED" || delivery.Status == "NOTIFIED") {
			return true
		}
	}
	return false
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
