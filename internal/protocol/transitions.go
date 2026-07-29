package protocol

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

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
	// env.set/env.delete write into the shared, append-only signed log with
	// no undo -- even an OBSERVER-role principal (intended read-only) could
	// otherwise write or delete arbitrary key/value data there. Owner-or-
	// orchestrator only, matching every other write with log-wide
	// consequences.
	return typ == "approval.approve" || typ == "approval.reject" || typ == "agent.activate" || typ == "agent.suspend" || typ == "agent.rotate-key" || typ == "agent.rename" || typ == "agent.revoke" || typ == "agent.delete" || typ == "project.settings.update" || typ == "env.set" || typ == "env.delete"
}
func hasApproval(st model.State, action string) bool {
	for _, a := range st.Approvals {
		if a.Action == action && a.Status == "APPROVED" {
			return true
		}
	}
	return false
}

// hasHumanApproval is hasApproval narrowed to HUMAN-tier approvals only. An
// ORCHESTRATOR-tier approval can be approved by any AGENT-principal
// orchestrator (see the elevated()/approval.approve handling below), so it
// cannot stand in for genuine human sign-off on the orchestrator grant itself.
func hasHumanApproval(st model.State, action string) bool {
	for _, a := range st.Approvals {
		if a.Action == action && a.Status == "APPROVED" && a.Tier == "HUMAN" {
			return true
		}
	}
	return false
}

// OrchestratorGrantApprovalAction is the approval.request Action string that
// must be APPROVED (tier HUMAN) before agent.activate may grant id the
// ORCHESTRATOR role. Exported so the CLI/MCP/TUI can point a caller at the
// exact command instead of duplicating the "agent.activate:"+id format.
func OrchestratorGrantApprovalAction(id string) string { return "agent.activate:" + id }

// RequiresElevatedKey reports whether actor/typ/id/payload is one of the
// transitions that must be signed with the actor's passphrase-protected
// elevated key (internal/identity.ElevatedActor) rather than its everyday
// key, when one is registered (model.Agent.ElevatedPublicKey != ""). This is
// the single classification both the client-side signer
// (internal/service.Service) and every server-side verifier (personal and
// Postgres authority backends) call, so they can never disagree about which
// key a given transition needed. Only ever reads st.Approvals[id] and
// st.Agents[id] (the target, never the actor's own record) -- see the
// comment on scopedElevationState in internal/authority/postgres.go before
// adding a read of any other field, since that backend deliberately passes
// a minimal, targeted state rather than the full project state here.
func RequiresElevatedKey(st model.State, actor, typ, id string, payload any) bool {
	switch typ {
	case "agent.activate":
		activation, ok := payload.(model.AgentActivated)
		return ok && activation.Role == model.RoleOrchestrator
	case "approval.approve":
		return st.Approvals[id].Tier == "HUMAN"
	case "agent.revoke":
		// Mirrors the human-only check ValidateTransition already applies to
		// agent.revoke: self-revocation is never an escalation and always
		// bypasses it, but revoking a different orchestrator or human
		// principal is exactly as sensitive as granting the role in the
		// first place -- it was defeatable by the same ambient-fallback /
		// local-credential-access gap agent.activate(ORCHESTRATOR) had
		// before the elevated key existed, and nothing had closed it there.
		if id == actor {
			return false
		}
		target := st.Agents[id]
		return target.Role == model.RoleOrchestrator || target.PrincipalType == model.PrincipalHuman
	case "agent.delete":
		target, found := st.Agents[id]
		return found && target.Status == "REVOKED"
	default:
		return false
	}
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

func runtimeKind(kind model.RuntimeKind, connector string) model.RuntimeKind {
	if kind != "" {
		return kind
	}
	if strings.EqualFold(connector, "INTERACTIVE") {
		return model.RuntimeKindInteractive
	}
	return model.RuntimeKindWorker
}

func validConsumerMode(mode model.ConsumerMode) bool {
	return mode == model.ConsumerModeInteractiveOnly ||
		mode == model.ConsumerModeWorkerOnly ||
		mode == model.ConsumerModeEither
}

func effectiveInvocationConsumer(mode model.ConsumerMode) model.ConsumerMode {
	if validConsumerMode(mode) {
		return mode
	}
	return model.ConsumerModeEither
}

func consumerAllowsKind(mode model.ConsumerMode, kind model.RuntimeKind) bool {
	mode = effectiveInvocationConsumer(mode)
	switch mode {
	case model.ConsumerModeInteractiveOnly:
		return kind == model.RuntimeKindInteractive
	case model.ConsumerModeWorkerOnly:
		return kind == model.RuntimeKindWorker
	case model.ConsumerModeEither:
		return kind == model.RuntimeKindWorker || kind == model.RuntimeKindInteractive
	default:
		return false
	}
}

func effectiveConsumerPolicy(policy model.InvocationPolicy) (model.ConsumerMode, []model.ConsumerMode) {
	defaultMode := policy.DefaultConsumerMode
	if !validConsumerMode(defaultMode) {
		defaultMode = model.ConsumerModeEither
	}
	allowed := append([]model.ConsumerMode(nil), policy.AllowedConsumerModes...)
	if len(allowed) == 0 {
		allowed = []model.ConsumerMode{
			model.ConsumerModeInteractiveOnly,
			model.ConsumerModeWorkerOnly,
			model.ConsumerModeEither,
		}
	}
	return defaultMode, allowed
}

func containsConsumerMode(values []model.ConsumerMode, target model.ConsumerMode) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func nextDeliveryAttempt(state model.State, invocationID string) int {
	next := 1
	for _, delivery := range state.InvocationDeliveries {
		if delivery.InvocationID == invocationID && delivery.Attempt >= next {
			next = delivery.Attempt + 1
		}
	}
	return next
}

func automaticDeliveryAttempts(state model.State, invocationID string) int {
	count := 0
	for _, delivery := range state.InvocationDeliveries {
		if delivery.InvocationID == invocationID && !delivery.Manual {
			count++
		}
	}
	return count
}

func runtimeInvocationLoad(state model.State, runtimeID string) int {
	load := 0
	for _, invocation := range state.Invocations {
		if invocation.RuntimeID != runtimeID {
			continue
		}
		if invocation.Status == "CLAIMED" || invocation.Status == "RUNNING" ||
			invocation.Status == "WAITING" {
			load++
		}
	}
	return load
}

func validateDeliveryEvidence(transport, endpointID string, evidence []model.DeliveryEvidence, attemptedAt, completedAt time.Time) error {
	if len(evidence) == 0 || len(evidence) > controlplane.MaxDeliveryEvidence {
		return fmt.Errorf("delivery evidence must contain from 1 to %d stages", controlplane.MaxDeliveryEvidence)
	}
	allowed := map[string]bool{"CONNECTOR_ACCEPTED": true}
	if transport == "INTERACTIVE" {
		allowed = map[string]bool{"PTY_TEXT_ECHOED": true, "PTY_ENTER_SENT": true}
		if strings.TrimSpace(endpointID) == "" {
			return errors.New("interactive delivery endpoint ID is required")
		}
	}
	seen := map[string]bool{}
	previous := attemptedAt
	for _, item := range evidence {
		stage := strings.ToUpper(strings.TrimSpace(item.Stage))
		if !allowed[stage] || seen[stage] {
			return fmt.Errorf("delivery evidence stage %q is invalid or duplicated for %s", item.Stage, transport)
		}
		if item.At.Before(attemptedAt) || item.At.After(completedAt.Add(controlplane.CommandClockSkew)) ||
			item.At.Before(previous) {
			return errors.New("delivery evidence timestamps must be monotonic and within the attempt window")
		}
		seen[stage] = true
		previous = item.At
	}
	if transport == "INTERACTIVE" && (!seen["PTY_TEXT_ECHOED"] || !seen["PTY_ENTER_SENT"]) {
		return errors.New("interactive delivery requires PTY_TEXT_ECHOED and PTY_ENTER_SENT evidence")
	}
	if transport != "INTERACTIVE" && !seen["CONNECTOR_ACCEPTED"] {
		return errors.New("connector delivery requires CONNECTOR_ACCEPTED evidence")
	}
	return nil
}

func normalizeRuntimeDefinition(kind model.RuntimeKind, connector, configReference, hostID string, maxConcurrent int) (model.RuntimeKind, string, error) {
	connector = strings.ToUpper(strings.TrimSpace(connector))
	kind = runtimeKind(kind, connector)
	if kind != model.RuntimeKindWorker && kind != model.RuntimeKindInteractive {
		return "", "", errors.New("runtime kind must be WORKER or INTERACTIVE")
	}
	validConnector := connector == "MANUAL" || connector == "MCP" ||
		connector == "LOCAL_PROCESS" || connector == "WEBHOOK" ||
		connector == "QUEUE" || connector == "INTERACTIVE"
	if !validConnector {
		return "", "", errors.New("runtime connector must be MANUAL, MCP, LOCAL_PROCESS, WEBHOOK, QUEUE, or INTERACTIVE")
	}
	if maxConcurrent < 1 || maxConcurrent > controlplane.MaxRuntimeConcurrency {
		return "", "", fmt.Errorf("runtime concurrency must be from 1 to %d", controlplane.MaxRuntimeConcurrency)
	}
	if (connector == "LOCAL_PROCESS" || connector == "WEBHOOK") && strings.TrimSpace(configReference) == "" {
		return "", "", fmt.Errorf("%s runtime requires a connector configuration reference", connector)
	}
	if strings.Contains(strings.ToLower(configReference), "secret") ||
		strings.Contains(strings.ToLower(configReference), "token") ||
		strings.Contains(strings.ToLower(configReference), "password") {
		return "", "", errors.New("runtime config reference must not contain secret material")
	}
	if kind == model.RuntimeKindInteractive {
		if connector != "INTERACTIVE" {
			return "", "", errors.New("INTERACTIVE runtime kind requires the INTERACTIVE connector")
		}
		if strings.TrimSpace(hostID) == "" {
			return "", "", errors.New("interactive runtime host ID is required")
		}
		if strings.TrimSpace(configReference) != "" {
			return "", "", errors.New("interactive runtime must not use a connector configuration reference")
		}
		if maxConcurrent != 1 {
			return "", "", errors.New("interactive runtime concurrency must be 1")
		}
	} else if connector == "INTERACTIVE" {
		return "", "", errors.New("INTERACTIVE connector requires the INTERACTIVE runtime kind")
	}
	return kind, connector, nil
}

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
		selfKeyRotation := typ == "agent.rotate-key" && id == actor
		selfRevoke := typ == "agent.revoke" && id == actor
		if elevated(typ) && !selfKeyRotation && !selfRevoke && a.Role != model.RoleOwner && a.Role != model.RoleOrchestrator {
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
		target, ok := st.Agents[id]
		if !ok {
			return nil, errors.New("pending principal not found")
		}
		if target.Status == "REVOKED" {
			return nil, errors.New("cannot activate a revoked principal")
		}
		activation, ok := payload.(model.AgentActivated)
		if !ok || (activation.Role != model.RoleOwner && activation.Role != model.RoleOrchestrator &&
			activation.Role != model.RoleAgent && activation.Role != model.RoleObserver) {
			return nil, errors.New("valid activation role is required")
		}
		// Granting the orchestrator role is a hard, human-only check on top
		// of the owner-or-orchestrator elevation already required above: an
		// existing orchestrator that is itself an AGENT principal (not a
		// human) must not be able to mint further orchestrators on its own.
		// Every orchestrator promotion requires a human in the loop, no
		// matter who initiates the call — this is checked here, the one
		// transition validator shared by every interface (CLI, MCP, TUI,
		// daemon) and both authority backends, not duplicated per-interface.
		if activation.Role == model.RoleOrchestrator && st.Agents[actor].PrincipalType != model.PrincipalHuman {
			return nil, errors.New("human principal required to grant the orchestrator role")
		}
		// The principal-type check above is defeated the moment an agent
		// operates over an unregistered, ambient-owner-fallback connection
		// (docs/agent-onboarding.md): the signing credential is genuinely
		// HUMAN by construction, so a fully autonomous agent session can
		// satisfy it without a human ever deciding anything in the moment.
		// Close that gap with a real two-step control: granting ORCHESTRATOR
		// additionally requires a pre-existing, separately-approved,
		// HUMAN-tier approval record for this exact grant. Anyone (including
		// an agent) may "apply" via `approval request --tier HUMAN --action
		// agent.activate:<id>`, but the approval itself must be granted in a
		// distinct, later action — giving a human a real request to see and
		// manually approve (e.g. in the TUI) before the grant can proceed,
		// rather than a single self-contained command completing the whole
		// escalation unattended.
		if activation.Role == model.RoleOrchestrator && !hasHumanApproval(st, OrchestratorGrantApprovalAction(id)) {
			return nil, fmt.Errorf("granting the orchestrator role to %s requires an approved HUMAN-tier approval first: run `approval request --tier HUMAN --action %s`, then have a human approve it separately", id, OrchestratorGrantApprovalAction(id))
		}
	}
	if typ == "agent.elevate-key" {
		// Self-service only: registering (or rotating, by re-registering) an
		// elevated key is never done on someone else's behalf, so this is
		// deliberately absent from elevated() -- no owner/orchestrator gate
		// applies, only the ordinary active-principal check already run
		// above. Restricted to HUMAN principals because it's meaningless
		// for an AGENT one: the transitions that actually consult
		// ElevatedPublicKey (RequiresElevatedKey) already independently
		// require the signing actor to be a HUMAN principal regardless of
		// which key it used, so an AGENT principal's elevated key could
		// never be reached anyway.
		if id != actor {
			return nil, errors.New("an elevated key can only be registered for your own actor")
		}
		if st.Agents[actor].PrincipalType != model.PrincipalHuman {
			return nil, errors.New("only a human principal may register an elevated key")
		}
		registered, ok := payload.(model.AgentElevatedKeyRegistered)
		if !ok || registered.PublicKey == "" {
			return nil, errors.New("a public key is required to register an elevated key")
		}
	}
	if typ == "agent.rotate-key" && id != actor {
		// Rotating a DIFFERENT principal's key is a full identity-hijack
		// primitive: the reducer (internal/projection/apply.go) applies
		// AgentKeyRotated.PublicKey to the target unconditionally, with no
		// proof the target consented to or controls the new key -- an
		// orchestrator could rotate the owner's key to one it holds and
		// permanently lock the real owner out, worse than revoke (which at
		// least leaves the record intact rather than replacing its identity
		// outright). No shipped interface (CLI, MCP, TUI) has ever exposed
		// id != actor here -- every caller already hard-codes self-rotation
		// -- so this closes a latent gap in the shared validator itself
		// rather than removing real functionality.
		return nil, errors.New("a key can only be rotated for your own actor")
	}
	if typ == "agent.rename" {
		target, ok := st.Agents[id]
		if !ok {
			return nil, errors.New("principal not found")
		}
		if target.Status == "REVOKED" {
			return nil, errors.New("cannot rename a revoked principal")
		}
		renamed, ok := payload.(model.AgentRenamed)
		if !ok || strings.TrimSpace(renamed.DisplayName) == "" {
			return nil, errors.New("display name is required")
		}
	}
	if typ == "agent.suspend" {
		target, ok := st.Agents[id]
		if !ok {
			return nil, errors.New("principal not found")
		}
		if target.Status == "REVOKED" {
			return nil, errors.New("cannot suspend a revoked principal")
		}
		// Suspension is temporary, unlike revocation, but an unprotected
		// owner-target is actually the more severe lockout: a suspended
		// principal fails active() (above) on every subsequent action,
		// including trying to reactivate itself -- if the owner is the
		// project's only human and gets suspended by a rogue
		// AGENT-principal orchestrator, there is no path back without
		// trusting some other principal to reactivate them. Mirrors
		// agent.revoke's protection below exactly, for the same reason.
		if target.Role == model.RoleOwner {
			return nil, errors.New("owner principal cannot be suspended")
		}
		if id != actor && (target.Role == model.RoleOrchestrator || target.PrincipalType == model.PrincipalHuman) &&
			st.Agents[actor].PrincipalType != model.PrincipalHuman {
			return nil, errors.New("human principal required to suspend an orchestrator or human principal")
		}
	}
	if typ == "agent.revoke" {
		target, ok := st.Agents[id]
		if !ok {
			return nil, errors.New("principal not found")
		}
		if target.Status == "REVOKED" {
			return nil, errors.New("principal is already revoked")
		}
		if target.Role == model.RoleOwner {
			return nil, errors.New("owner principal cannot be revoked")
		}
		// Revoking an orchestrator or any human principal is a hard,
		// human-only check, symmetric to the human-only check on granting
		// the orchestrator role above: an AGENT-principal orchestrator must
		// not be able to unilaterally strip another orchestrator or a human
		// of standing to entrench itself. Self-revocation (a principal
		// voluntarily removing itself) is not an escalation and always
		// bypasses this, mirroring the ordinary elevation bypass above.
		if id != actor && (target.Role == model.RoleOrchestrator || target.PrincipalType == model.PrincipalHuman) &&
			st.Agents[actor].PrincipalType != model.PrincipalHuman {
			return nil, errors.New("human principal required to revoke an orchestrator or human principal")
		}
	}
	if typ == "agent.delete" {
		target, ok := st.Agents[id]
		if !ok {
			return nil, errors.New("principal not found")
		}
		if target.Status != "REVOKED" {
			return nil, errors.New("principal must be revoked before deletion")
		}
		if st.Agents[actor].PrincipalType != model.PrincipalHuman {
			return nil, errors.New("human principal required to delete a principal")
		}
		deleted, ok := payload.(model.AgentDeleted)
		if !ok || strings.TrimSpace(deleted.Reason) == "" {
			return nil, errors.New("deletion reason is required")
		}
	}
	if typ == "project.settings.update" {
		// An AGENT-principal orchestrator could otherwise unilaterally
		// weaken project-wide safety settings (e.g. disabling RequireReview)
		// -- combined with task.create's self-declared, ungated Risk field,
		// that's enough for an orchestrator agent to label its own work
		// ROUTINE and then turn off the review requirement that would
		// otherwise have caught it. Not gated behind the elevated key (that
		// stays reserved for the narrow RequiresElevatedKey
		// classification), just a
		// human-principal requirement, matching the bar already set for
		// revoking/suspending an orchestrator or human principal.
		if st.Agents[actor].PrincipalType != model.PrincipalHuman {
			return nil, errors.New("human principal required to update project settings")
		}
		settings, ok := payload.(model.ProjectSettingsUpdated)
		if !ok {
			return nil, errors.New("invalid project settings payload")
		}
		lease, leaseErr := time.ParseDuration(settings.DefaultLease)
		grace, graceErr := time.ParseDuration(settings.StaleGrace)
		retention, retentionErr := time.ParseDuration(settings.ActiveRetention)
		if leaseErr != nil || lease < 15*time.Minute || lease > 24*time.Hour {
			return nil, errors.New("default lease must be between 15m and 24h")
		}
		if graceErr != nil || grace < 5*time.Minute || grace > 24*time.Hour {
			return nil, errors.New("stale grace must be between 5m and 24h")
		}
		if retentionErr != nil || retention < 24*time.Hour || retention > 8760*time.Hour {
			return nil, errors.New("active retention must be between 24h and 8760h")
		}
		if settings.SummaryLimit < 256 || settings.SummaryLimit > controlplane.MaxCommandBytes {
			return nil, fmt.Errorf("summary limit must be between 256 and %d", controlplane.MaxCommandBytes)
		}
		const minArtifactBytes = 1024
		if settings.ArtifactLimitBytes < minArtifactBytes || settings.ArtifactLimitBytes > controlplane.MaxDraftStorageBytes {
			return nil, fmt.Errorf("artifact limit must be between %d and %d bytes", minArtifactBytes, controlplane.MaxDraftStorageBytes)
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
			settings := model.EffectiveProjectSettings(st.ProjectSettings)
			defaultLease, _ := time.ParseDuration(settings.DefaultLease)
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
			p.LeaseUntil = now.Add(defaultLease)
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
			settings := model.EffectiveProjectSettings(st.ProjectSettings)
			defaultLease, _ := time.ParseDuration(settings.DefaultLease)
			if t.Owner != actor {
				return nil, errors.New("task owner required")
			}
			if strings.TrimSpace(p.Progress) == "" {
				return nil, errors.New("progress summary is required")
			}
			p.LeaseUntil = now.Add(defaultLease)
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
			settings := model.EffectiveProjectSettings(st.ProjectSettings)
			if typ == "task.complete" && (t.Risk != "ROUTINE" || settings.RequireReview) {
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
			if p.TaskID != "" {
				task, taskExists := st.Tasks[p.TaskID]
				if !taskExists {
					return nil, errors.New("related task not found")
				}
				if len(p.Scopes) == 0 {
					p.Scopes = append([]string(nil), task.Resources...)
				}
			}
			if len(p.Scopes) > 0 {
				if !scopeAllows(requester.Scopes, p.Scopes) && !actorElevated(st, actor) {
					return nil, errors.New("invocation scopes exceed requester scopes")
				}
				if !scopeAllows(target.Scopes, p.Scopes) {
					return nil, errors.New("invocation scopes exceed target scopes")
				}
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
			policy := st.InvocationPolicies[p.Target]
			if len(policy.AllowedScopes) > 0 && !scopeAllows(policy.AllowedScopes, p.Scopes) {
				return nil, errors.New("invocation scopes exceed target policy")
			}
			if policy.RequireHumanForSensitive && invocationIsSensitive(st, p) &&
				requester.PrincipalType != model.PrincipalHuman &&
				!hasApproval(st, "invocation-sensitive:"+id) {
				return nil, errors.New("human approval is required for sensitive invocation")
			}
			defaultConsumer, allowedConsumers := effectiveConsumerPolicy(policy)
			if p.ConsumerMode == "" {
				p.ConsumerMode = defaultConsumer
			}
			p.ConsumerMode = model.ConsumerMode(strings.ToUpper(strings.TrimSpace(string(p.ConsumerMode))))
			if !validConsumerMode(p.ConsumerMode) {
				return nil, errors.New("invocation consumer mode must be INTERACTIVE_ONLY, WORKER_ONLY, or EITHER")
			}
			if !containsConsumerMode(allowedConsumers, p.ConsumerMode) {
				return nil, errors.New("invocation consumer mode is not allowed by target policy")
			}
			if p.PreferredRuntimeID == "" && p.ConsumerMode != model.ConsumerModeWorkerOnly {
				p.PreferredRuntimeID = policy.PreferredInteractiveRuntimeID
			}
			if p.PreferredRuntimeID != "" {
				preferred, preferredExists := st.AgentRuntimes[p.PreferredRuntimeID]
				if !preferredExists || preferred.AgentID != p.Target ||
					!consumerAllowsKind(p.ConsumerMode, runtimeKind(preferred.Kind, preferred.Connector)) {
					return nil, errors.New("preferred runtime must belong to the target and match the invocation consumer mode")
				}
				if preferred.Status == "REVOKED" {
					return nil, errors.New("preferred runtime is revoked")
				}
			}
			payload = p
		case model.InvocationDeliveryAttempted:
			if !exists {
				return nil, errors.New("invocation not found")
			}
			if invocation.Status != "PENDING" && invocation.Status != "NOTIFIED" {
				return nil, fmt.Errorf("cannot attempt delivery while invocation is %s", invocation.Status)
			}
			if actor != invocation.RequestedBy && actor != invocation.Target && !actorElevated(st, actor) {
				return nil, errors.New("invocation requester, target, owner, or orchestrator required")
			}
			if strings.TrimSpace(p.DeliveryID) == "" || strings.TrimSpace(p.RuntimeID) == "" {
				return nil, errors.New("delivery ID and runtime ID are required")
			}
			if _, duplicate := st.InvocationDeliveries[p.DeliveryID]; duplicate {
				return nil, errors.New("invocation delivery already exists")
			}
			runtime, runtimeExists := st.AgentRuntimes[p.RuntimeID]
			if !runtimeExists || runtime.AgentID != invocation.Target {
				return nil, errors.New("delivery runtime must belong to the invocation target")
			}
			kind := runtimeKind(runtime.Kind, runtime.Connector)
			if !consumerAllowsKind(effectiveInvocationConsumer(invocation.ConsumerMode), kind) {
				return nil, errors.New("delivery runtime does not match the invocation consumer mode")
			}
			if invocation.PreferredRuntimeID != "" && invocation.PreferredRuntimeID != p.RuntimeID {
				return nil, errors.New("delivery runtime does not match the preferred runtime")
			}
			if runtime.Status == "DRAINING" || runtime.Status == "REVOKED" {
				return nil, fmt.Errorf("delivery runtime is %s", strings.ToLower(runtime.Status))
			}
			if kind == model.RuntimeKindInteractive && runtime.Status != "ONLINE" {
				return nil, errors.New("interactive delivery runtime must be online")
			}
			p.Transport = strings.ToUpper(strings.TrimSpace(p.Transport))
			if p.Transport == "" {
				p.Transport = runtime.Connector
			}
			if p.Transport != runtime.Connector {
				return nil, errors.New("delivery transport must match the registered runtime connector")
			}
			if p.Transport == "MANUAL" || p.Transport == "MCP" || p.Transport == "QUEUE" {
				return nil, fmt.Errorf("%s runtime does not support notification delivery attempts", p.Transport)
			}
			if p.HostID == "" {
				p.HostID = runtime.HostID
			}
			if p.HostID != runtime.HostID {
				return nil, errors.New("delivery host ID must match the registered runtime")
			}
			if kind == model.RuntimeKindInteractive {
				if strings.TrimSpace(runtime.EndpointID) == "" {
					return nil, errors.New("interactive delivery runtime has no active endpoint")
				}
				if p.EndpointID == "" {
					p.EndpointID = runtime.EndpointID
				}
				if p.EndpointID != runtime.EndpointID {
					return nil, errors.New("delivery endpoint ID must match the active interactive runtime endpoint")
				}
			} else if strings.TrimSpace(p.EndpointID) != "" {
				return nil, errors.New("worker delivery attempt must not contain an interactive endpoint ID")
			}
			for _, delivery := range st.InvocationDeliveries {
				if delivery.InvocationID == id && delivery.RuntimeID == p.RuntimeID &&
					delivery.Status == "ATTEMPTED" && delivery.AttemptUntil != nil &&
					delivery.AttemptUntil.After(now) {
					return nil, errors.New("an unexpired delivery attempt already exists for this runtime")
				}
			}
			if !p.Manual && automaticDeliveryAttempts(st, id) >= controlplane.MaxDeliveryAttempts {
				return nil, fmt.Errorf("automatic delivery attempts exhausted at %d", controlplane.MaxDeliveryAttempts)
			}
			p.Attempt = nextDeliveryAttempt(st, id)
			if p.AttemptUntil.IsZero() {
				p.AttemptUntil = now.Add(controlplane.DefaultDeliveryAttemptLease)
			}
			if !p.AttemptUntil.After(now) ||
				p.AttemptUntil.After(now.Add(controlplane.MaxDeliveryAttemptLease)) {
				return nil, fmt.Errorf("delivery attempt lease must be within %s", controlplane.MaxDeliveryAttemptLease)
			}
			payload = p
		case model.InvocationNotified:
			if !exists {
				return nil, errors.New("invocation not found")
			}
			if actor != invocation.RequestedBy && actor != invocation.Target && !actorElevated(st, actor) {
				return nil, errors.New("invocation requester, target, owner, or orchestrator required")
			}
			if strings.TrimSpace(p.DeliveryID) == "" {
				return nil, errors.New("delivery ID is required")
			}
			delivery, deliveryExists := st.InvocationDeliveries[p.DeliveryID]
			if !deliveryExists || delivery.InvocationID != id || delivery.Status != "ATTEMPTED" {
				return nil, errors.New("matching attempted delivery is required")
			}
			if delivery.AttemptUntil == nil || !delivery.AttemptUntil.After(now) {
				return nil, errors.New("delivery attempt lease has expired")
			}
			if delivery.AttemptedAt == nil {
				return nil, errors.New("delivery attempt is missing its reservation timestamp")
			}
			p.RuntimeID = delivery.RuntimeID
			p.Attempt = delivery.Attempt
			p.Transport = delivery.Transport
			if p.Transport == "INTERACTIVE" && p.EndpointID != delivery.EndpointID {
				return nil, errors.New("interactive delivery evidence does not match the reserved endpoint")
			}
			if err := validateDeliveryEvidence(
				p.Transport, p.EndpointID, p.Evidence, *delivery.AttemptedAt, now,
			); err != nil {
				return nil, err
			}
			payload = p
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
			runtime, runtimeExists := st.AgentRuntimes[p.RuntimeID]
			if !runtimeExists || runtime.AgentID != invocation.Target {
				return nil, errors.New("claiming runtime must be registered to the invocation target")
			}
			if runtime.Status != "ONLINE" || runtime.Health == "UNKNOWN" {
				return nil, errors.New("claiming runtime must be online")
			}
			if runtimeInvocationLoad(st, p.RuntimeID) >= runtime.MaxConcurrent {
				return nil, errors.New("claiming runtime has no available capacity")
			}
			if !consumerAllowsKind(effectiveInvocationConsumer(invocation.ConsumerMode), runtimeKind(runtime.Kind, runtime.Connector)) {
				return nil, errors.New("claiming runtime does not match the invocation consumer mode")
			}
			if invocation.PreferredRuntimeID != "" && invocation.PreferredRuntimeID != p.RuntimeID {
				return nil, errors.New("claiming runtime does not match the preferred runtime")
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
			} else if typ == "invocation.cancel" {
				if actor != invocation.RequestedBy && !actorElevated(st, actor) {
					return nil, errors.New("invocation requester, owner, or orchestrator required")
				}
				if invocation.Status == "COMPLETED" || invocation.Status == "REJECTED" ||
					invocation.Status == "EXPIRED" || invocation.Status == "CANCELLED" ||
					invocation.Status == "DEAD_LETTER" {
					return nil, fmt.Errorf("cannot cancel invocation while %s", invocation.Status)
				}
			}
		case model.InvocationDeliveryFailed:
			if !exists {
				return nil, errors.New("invocation not found")
			}
			if actor != invocation.RequestedBy && actor != invocation.Target && !actorElevated(st, actor) {
				return nil, errors.New("invocation requester, target, owner, or orchestrator required")
			}
			if strings.TrimSpace(p.DeliveryID) == "" || strings.TrimSpace(p.Error) == "" {
				return nil, errors.New("valid delivery ID, attempt, and error are required")
			}
			delivery, deliveryExists := st.InvocationDeliveries[p.DeliveryID]
			if !deliveryExists || delivery.InvocationID != id || delivery.Status != "ATTEMPTED" {
				return nil, errors.New("matching attempted delivery is required")
			}
			p.RuntimeID = delivery.RuntimeID
			p.Attempt = delivery.Attempt
			if !p.Final && (p.NextRetry == nil || !p.NextRetry.After(now)) {
				return nil, errors.New("future retry time is required for a retryable delivery failure")
			}
			payload = p
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
			var definitionErr error
			p.Kind, p.Connector, definitionErr = normalizeRuntimeDefinition(
				p.Kind, p.Connector, p.ConfigReference, p.HostID, p.MaxConcurrent,
			)
			if definitionErr != nil {
				return nil, definitionErr
			}
			payload = p
		case model.RuntimeConfigured:
			if typ != "runtime.configure" {
				return nil, errors.New("invalid runtime configuration transition")
			}
			if !exists {
				return nil, errors.New("runtime not found")
			}
			if actor != runtime.AgentID && !actorElevated(st, actor) {
				return nil, errors.New("runtime owner, project owner, or orchestrator required")
			}
			if runtime.Status != "OFFLINE" && runtime.Status != "DRAINING" {
				return nil, errors.New("runtime must be offline or draining before configuration")
			}
			if len(runtime.ActiveInvocations) != 0 || runtimeInvocationLoad(st, id) != 0 {
				return nil, errors.New("runtime with active invocations cannot be configured")
			}
			var definitionErr error
			p.Kind, p.Connector, definitionErr = normalizeRuntimeDefinition(
				p.Kind, p.Connector, p.ConfigReference, p.HostID, p.MaxConcurrent,
			)
			if definitionErr != nil {
				return nil, definitionErr
			}
			payload = p
		case model.RuntimeHeartbeat:
			if !exists || actor != runtime.AgentID {
				return nil, errors.New("registered runtime owner required")
			}
			if runtime.Status == "REVOKED" || runtime.Status == "DRAINING" {
				return nil, fmt.Errorf("runtime cannot heartbeat while %s", runtime.Status)
			}
			if runtime.Status == "ONLINE" && !runtime.LastSeenAt.IsZero() &&
				now.Sub(runtime.LastSeenAt) < controlplane.MinHeartbeatInterval {
				return nil, fmt.Errorf("runtime heartbeat interval must be at least %s", controlplane.MinHeartbeatInterval)
			}
			p.Health = strings.ToUpper(strings.TrimSpace(p.Health))
			if p.Health != "HEALTHY" && p.Health != "DEGRADED" {
				return nil, errors.New("runtime health must be HEALTHY or DEGRADED")
			}
			if len(p.ActiveInvocations) > runtime.MaxConcurrent {
				return nil, errors.New("active invocations exceed runtime concurrency")
			}
			if runtimeKind(runtime.Kind, runtime.Connector) == model.RuntimeKindInteractive {
				if strings.TrimSpace(p.EndpointID) == "" {
					return nil, errors.New("interactive runtime endpoint ID is required")
				}
			} else if strings.TrimSpace(p.EndpointID) != "" {
				return nil, errors.New("worker runtime heartbeat must not contain an interactive endpoint ID")
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
				if typ == "runtime.offline" {
					if runtime.Status == "REVOKED" {
						return nil, errors.New("revoked runtime cannot transition offline")
					}
					if runtimeKind(runtime.Kind, runtime.Connector) == model.RuntimeKindInteractive &&
						runtime.Status == "ONLINE" &&
						(strings.TrimSpace(p.EndpointID) == "" || p.EndpointID != runtime.EndpointID) {
						return nil, errors.New("interactive runtime offline transition must match the active endpoint")
					}
					break
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
		if policy.DefaultConsumerMode == "" {
			policy.DefaultConsumerMode = model.ConsumerModeEither
		}
		policy.DefaultConsumerMode = model.ConsumerMode(strings.ToUpper(
			strings.TrimSpace(string(policy.DefaultConsumerMode)),
		))
		if !validConsumerMode(policy.DefaultConsumerMode) {
			return nil, errors.New("default consumer mode must be INTERACTIVE_ONLY, WORKER_ONLY, or EITHER")
		}
		if len(policy.AllowedConsumerModes) == 0 {
			policy.AllowedConsumerModes = []model.ConsumerMode{
				model.ConsumerModeInteractiveOnly,
				model.ConsumerModeWorkerOnly,
				model.ConsumerModeEither,
			}
		}
		seenConsumerModes := map[model.ConsumerMode]bool{}
		for index, consumerMode := range policy.AllowedConsumerModes {
			consumerMode = model.ConsumerMode(strings.ToUpper(strings.TrimSpace(string(consumerMode))))
			if !validConsumerMode(consumerMode) || seenConsumerModes[consumerMode] {
				return nil, errors.New("allowed consumer modes must be unique INTERACTIVE_ONLY, WORKER_ONLY, or EITHER values")
			}
			seenConsumerModes[consumerMode] = true
			policy.AllowedConsumerModes[index] = consumerMode
		}
		if !seenConsumerModes[policy.DefaultConsumerMode] {
			return nil, errors.New("default consumer mode must be included in allowed consumer modes")
		}
		if policy.PreferredInteractiveRuntimeID != "" {
			preferred, preferredExists := st.AgentRuntimes[policy.PreferredInteractiveRuntimeID]
			if !preferredExists || preferred.AgentID != id ||
				runtimeKind(preferred.Kind, preferred.Connector) != model.RuntimeKindInteractive ||
				preferred.Status == "REVOKED" {
				return nil, errors.New("preferred interactive runtime must be a non-revoked interactive runtime owned by the target")
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

func invocationIsSensitive(state model.State, request model.InvocationRequested) bool {
	if request.Priority == "URGENT" {
		return true
	}
	if request.TaskID == "" {
		return false
	}
	task, exists := state.Tasks[request.TaskID]
	return exists && task.Risk != "" && task.Risk != "ROUTINE"
}

func RefreshRuntimePresence(state *model.State, now time.Time) {
	for id, runtime := range state.AgentRuntimes {
		if runtime.Status == "ONLINE" && !runtime.LastSeenAt.IsZero() &&
			now.Sub(runtime.LastSeenAt) > controlplane.RuntimeOfflineAfter {
			runtime.Status = "OFFLINE"
			runtime.Health = "UNKNOWN"
			runtime.EndpointID = ""
			runtime.ActiveInvocations = nil
			runtime.Reason = "heartbeat expired"
			state.AgentRuntimes[id] = runtime
		}
	}
}
