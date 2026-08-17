package main

import (
	"fmt"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

// The AXIOM/DAMON/GORGE demo agents and the story they tell, reproduced
// through real service.Execute transition calls -- not hand-authored
// fixtures -- matching sites/landing/src/components/ControlRoomFrame.tsx's
// workforce/activity arrays:
//
//   - AXIOM (Release-Coordinator): online, an active runtime, mid-invocation
//     from owner, and requesting its own HUMAN-tier ORCHESTRATOR approval --
//     left PENDING for a live visitor to approve or reject.
//   - DAMON (Frontend-Architect): online, an active runtime, self-switched
//     into its role, and holding a claimed task (test/auth).
//   - GORGE (Tester): registered and activated, but never given a runtime --
//     shows up OFFLINE, exactly as the landing page's static story has it.
const (
	agentAxiom = "AXIOM"
	agentDamon = "DAMON"
	agentGorge = "GORGE"

	axiomRuntimeID = "axiom-runtime-1"
	damonRuntimeID = "damon-runtime-1"

	authTaskID          = "task-auth-session"
	pendingInvocationID = "inv-axiom-release"
	pendingApprovalID   = "approval-orchestrator-axiom"
	gorgeMessageID      = "msg-axiom-gorge"
)

// seedDemoProject drives the AXIOM/DAMON/GORGE story into s through real
// Execute/Register calls, in narrative order, and returns the first error
// encountered -- a broken seed should never silently produce a half-empty
// demo. Leaves one HUMAN-tier approval and one invocation genuinely pending
// so a live visitor has something real to act on.
func seedDemoProject(s *service.Service) error {
	if err := seedAgents(s); err != nil {
		return fmt.Errorf("seed agents: %w", err)
	}
	if err := seedRuntimes(s); err != nil {
		return fmt.Errorf("seed runtimes: %w", err)
	}
	if err := seedTaskHandoff(s); err != nil {
		return fmt.Errorf("seed task handoff: %w", err)
	}
	if err := seedActivityPrelude(s); err != nil {
		return fmt.Errorf("seed activity prelude: %w", err)
	}
	if err := seedPendingInvocation(s); err != nil {
		return fmt.Errorf("seed pending invocation: %w", err)
	}
	if err := seedPendingApproval(s); err != nil {
		return fmt.Errorf("seed pending approval: %w", err)
	}
	if err := seedMessage(s); err != nil {
		return fmt.Errorf("seed message: %w", err)
	}
	return nil
}

// seedAgents registers and activates AXIOM, DAMON, and GORGE. DAMON is
// activated under a placeholder role here -- seedActivityPrelude switches it
// to its real "Frontend-Architect" label, mirroring
// ControlRoomFrame.tsx's activity seq 0142 (agent.switch-role).
func seedAgents(s *service.Service) error {
	if _, err := s.Register(agentAxiom, agentAxiom, model.PrincipalAgent); err != nil {
		return err
	}
	if _, err := s.Execute(demoOwner, "agent.activate", agentAxiom, model.AgentActivated{
		Role: model.Role("Release-Coordinator"), Capabilities: []string{"*"}, Scopes: []string{"*"},
	}); err != nil {
		return err
	}

	if _, err := s.Register(agentDamon, agentDamon, model.PrincipalAgent); err != nil {
		return err
	}
	if _, err := s.Execute(demoOwner, "agent.activate", agentDamon, model.AgentActivated{
		Role: model.Role("MEMBER"), Capabilities: []string{"*"}, Scopes: []string{"*"},
	}); err != nil {
		return err
	}

	if _, err := s.Register(agentGorge, agentGorge, model.PrincipalAgent); err != nil {
		return err
	}
	if _, err := s.Execute(demoOwner, "agent.activate", agentGorge, model.AgentActivated{
		Role: model.Role("Tester"), Capabilities: []string{"*"}, Scopes: []string{"*"},
	}); err != nil {
		return err
	}
	return nil
}

// seedRuntimes registers an ONLINE, healthy runtime for AXIOM and DAMON
// only -- GORGE stays offline/unregistered as a runtime, per the story.
func seedRuntimes(s *service.Service) error {
	if err := seedOnlineRuntime(s, agentAxiom, axiomRuntimeID, 2); err != nil {
		return err
	}
	return seedOnlineRuntime(s, agentDamon, damonRuntimeID, 1)
}

func seedOnlineRuntime(s *service.Service, agentID, runtimeID string, maxConcurrent int) error {
	if _, err := s.Execute(agentID, "runtime.register", runtimeID, model.RuntimeRegistered{
		AgentID: agentID, Connector: "MANUAL", MaxConcurrent: maxConcurrent,
	}); err != nil {
		return err
	}
	_, err := s.Execute(agentID, "runtime.heartbeat", runtimeID, model.RuntimeHeartbeat{Health: "HEALTHY"})
	return err
}

// seedTaskHandoff creates the test/auth task and has DAMON claim it,
// matching the workforce table's "test/auth" current-work entry.
func seedTaskHandoff(s *service.Service) error {
	if _, err := s.Execute(demoOwner, "task.create", authTaskID, model.TaskCreated{
		Title: "test/auth", Repository: "agent-comms-demo", Branch: "test/auth",
		Resources: []string{"auth"}, Risk: "ROUTINE",
	}); err != nil {
		return err
	}
	_, err := s.Execute(agentDamon, "task.claim", authTaskID, model.TaskClaimed{})
	return err
}

// seedActivityPrelude switches DAMON into its real "Frontend-Architect"
// label -- ControlRoomFrame.tsx's activity seq 0142 (agent.switch-role,
// actor "DAMON · OWNER").
func seedActivityPrelude(s *service.Service) error {
	_, err := s.Execute(agentDamon, "agent.switch-role", agentDamon, model.AgentRoleSwitched{
		Role: model.Role("Frontend-Architect"),
	})
	return err
}

// seedPendingInvocation drives owner -> AXIOM through
// invocation.request/claim/start, mirroring ControlRoomFrame.tsx's activity
// seq 0144-0147, and deliberately stops there: invocation.complete is never
// called, leaving a real RUNNING invocation a live visitor can still act on.
func seedPendingInvocation(s *service.Service) error {
	if _, err := s.Execute(demoOwner, "invocation.request", pendingInvocationID, model.InvocationRequested{
		Target: agentAxiom, Instruction: "Coordinate the auth-session release",
		ExpectedResult: "Confirm auth/session is ready to ship", Priority: "HIGH",
	}); err != nil {
		return err
	}
	if _, err := s.Execute(agentAxiom, "invocation.claim", pendingInvocationID, model.InvocationClaimed{
		RuntimeID: axiomRuntimeID,
	}); err != nil {
		return err
	}
	_, err := s.Execute(agentAxiom, "invocation.start", pendingInvocationID, model.InvocationProgress{
		Summary: "Coordinating the auth/session release",
	})
	return err
}

// seedPendingApproval has AXIOM request the exact HUMAN-tier approval
// protocol.OrchestratorGrantApprovalAction expects for its own
// ORCHESTRATOR grant, matching ControlRoomFrame.tsx's attention panel
// (REQUESTER AXIOM, ACTION agent.activate:AXIOM, ROLE ORCHESTRATOR, REASON
// "Coordinate the auth-session release"). Left PENDING -- approval.approve
// is never called here, so a live visitor has a real decision to make.
func seedPendingApproval(s *service.Service) error {
	_, err := s.Execute(agentAxiom, "approval.request", pendingApprovalID, model.ApprovalRequested{
		Tier: "HUMAN", Action: protocol.OrchestratorGrantApprovalAction(agentAxiom),
		Reason: "Coordinate the auth-session release",
	})
	return err
}

// seedMessage posts AXIOM's FYI to GORGE, the real-transition equivalent of
// ControlRoomFrame.tsx's activity seq 0148 ("AXIOM → GORGE") -- there is no
// "message.deliver" transition type in this codebase; message.post is the
// real primitive a delivered message is built from.
func seedMessage(s *service.Service) error {
	_, err := s.Execute(agentAxiom, "message.post", gorgeMessageID, model.MessagePosted{
		Kind: "FYI", To: []string{agentGorge}, Subject: "Auth/session release coordination",
		Body: "Flagging the auth/session release for your visibility once you're back online.",
	})
	return err
}
