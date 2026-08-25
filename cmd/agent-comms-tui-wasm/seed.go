package main

import (
	"fmt"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

// The reviewer/developer/tester demo agents and the story they tell,
// reproduced through real service.Execute transition calls -- not
// hand-authored fixtures -- matching
// sites/landing/src/components/ControlRoomFrame.tsx's workforce/activity
// arrays. These are generic, role-descriptive identities (not a real
// project's actual agent names) deliberately, so nothing here reads as a
// canonical or reserved example id -- docs/site's own CLI examples use
// placeholder tokens for the same reason. Kept short on purpose:
// internal/tui/model.go's real workforce table truncates the AGENT column
// to 13 characters (see workforce()'s `truncate(name, 13)`), a genuine
// constraint any real user's agent ids are already subject to -- "reviewer"/
// "developer"/"tester" all render in full, unlike the "agent-"-prefixed
// forms first tried here, which got cut to "agent-develo…" mid-word.
//
//   - reviewer (Release-Coordinator): online, an active runtime,
//     mid-invocation from owner, and requesting its own HUMAN-tier
//     ORCHESTRATOR approval -- left PENDING for a live visitor to approve or
//     reject.
//   - developer (Frontend-Architect): online, an active runtime,
//     self-switched into its role, and holding a claimed task (test/auth).
//   - tester (Tester): registered and activated, but never given a
//     runtime -- shows up OFFLINE, exactly as the landing page's static
//     story has it.
const (
	agentReviewer  = "reviewer"
	agentDeveloper = "developer"
	agentTester    = "tester"

	reviewerRuntimeID  = "reviewer-runtime-1"
	developerRuntimeID = "developer-runtime-1"

	authTaskID          = "task-auth-session"
	pendingInvocationID = "inv-release-coordination"
	pendingApprovalID   = "approval-orchestrator-reviewer"
	reviewMessageID     = "msg-reviewer-tester"
)

// seedDemoProject drives the reviewer/developer/tester story into s through
// real Execute/Register calls, in narrative order, and returns the first
// error encountered -- a broken seed should never silently produce a
// half-empty demo. Leaves one HUMAN-tier approval and one invocation
// genuinely pending so a live visitor has something real to act on.
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

// seedAgents registers and activates reviewer, developer, and tester.
// developer is activated under a placeholder role here -- seedActivityPrelude
// switches it to its real "Frontend-Architect" label, mirroring
// ControlRoomFrame.tsx's activity seq 0142 (agent.switch-role).
func seedAgents(s *service.Service) error {
	if _, err := s.Register(agentReviewer, agentReviewer, model.PrincipalAgent); err != nil {
		return err
	}
	if _, err := s.Execute(demoOwner, "agent.activate", agentReviewer, model.AgentActivated{
		Role: model.Role("Release-Coordinator"), Capabilities: []string{"*"}, Scopes: []string{"*"},
	}); err != nil {
		return err
	}

	if _, err := s.Register(agentDeveloper, agentDeveloper, model.PrincipalAgent); err != nil {
		return err
	}
	if _, err := s.Execute(demoOwner, "agent.activate", agentDeveloper, model.AgentActivated{
		Role: model.Role("MEMBER"), Capabilities: []string{"*"}, Scopes: []string{"*"},
	}); err != nil {
		return err
	}

	if _, err := s.Register(agentTester, agentTester, model.PrincipalAgent); err != nil {
		return err
	}
	if _, err := s.Execute(demoOwner, "agent.activate", agentTester, model.AgentActivated{
		Role: model.Role("Tester"), Capabilities: []string{"*"}, Scopes: []string{"*"},
	}); err != nil {
		return err
	}
	return nil
}

// seedRuntimes registers an ONLINE, healthy runtime for reviewer and
// developer only -- tester stays offline/unregistered as a runtime, per the
// story.
func seedRuntimes(s *service.Service) error {
	if err := seedOnlineRuntime(s, agentReviewer, reviewerRuntimeID, 2); err != nil {
		return err
	}
	return seedOnlineRuntime(s, agentDeveloper, developerRuntimeID, 1)
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

// seedTaskHandoff creates the test/auth task and has developer claim it,
// matching the workforce table's "test/auth" current-work entry.
func seedTaskHandoff(s *service.Service) error {
	if _, err := s.Execute(demoOwner, "task.create", authTaskID, model.TaskCreated{
		Title: "test/auth", Repository: "agent-comms-demo", Branch: "test/auth",
		Resources: []string{"auth"}, Risk: "ROUTINE",
	}); err != nil {
		return err
	}
	_, err := s.Execute(agentDeveloper, "task.claim", authTaskID, model.TaskClaimed{})
	return err
}

// seedActivityPrelude switches developer into its real "Frontend-Architect"
// label -- ControlRoomFrame.tsx's activity seq 0142 (agent.switch-role,
// actor "developer · OWNER").
func seedActivityPrelude(s *service.Service) error {
	_, err := s.Execute(agentDeveloper, "agent.switch-role", agentDeveloper, model.AgentRoleSwitched{
		Role: model.Role("Frontend-Architect"),
	})
	return err
}

// seedPendingInvocation drives owner -> reviewer through
// invocation.request/claim/start, mirroring ControlRoomFrame.tsx's activity
// seq 0144-0147, and deliberately stops there: invocation.complete is never
// called, leaving a real RUNNING invocation a live visitor can still act on.
func seedPendingInvocation(s *service.Service) error {
	if _, err := s.Execute(demoOwner, "invocation.request", pendingInvocationID, model.InvocationRequested{
		Target: agentReviewer, Instruction: "Coordinate the auth-session release",
		ExpectedResult: "Confirm auth/session is ready to ship", Priority: "HIGH",
	}); err != nil {
		return err
	}
	if _, err := s.Execute(agentReviewer, "invocation.claim", pendingInvocationID, model.InvocationClaimed{
		RuntimeID: reviewerRuntimeID,
	}); err != nil {
		return err
	}
	_, err := s.Execute(agentReviewer, "invocation.start", pendingInvocationID, model.InvocationProgress{
		Summary: "Coordinating the auth/session release",
	})
	return err
}

// seedPendingApproval has reviewer request the exact HUMAN-tier approval
// protocol.OrchestratorGrantApprovalAction expects for its own ORCHESTRATOR
// grant, matching ControlRoomFrame.tsx's attention panel (REQUESTER
// reviewer, ACTION agent.activate:reviewer, ROLE ORCHESTRATOR, REASON
// "Coordinate the auth-session release"). Left PENDING -- approval.approve
// is never called here, so a live visitor has a real decision to make.
func seedPendingApproval(s *service.Service) error {
	_, err := s.Execute(agentReviewer, "approval.request", pendingApprovalID, model.ApprovalRequested{
		Tier: "HUMAN", Action: protocol.OrchestratorGrantApprovalAction(agentReviewer),
		Reason: "Coordinate the auth-session release",
	})
	return err
}

// seedMessage posts reviewer's FYI to tester, the real-transition
// equivalent of ControlRoomFrame.tsx's activity seq 0148 ("reviewer →
// tester") -- there is no "message.deliver" transition type in this
// codebase; message.post is the real primitive a delivered message is built
// from.
func seedMessage(s *service.Service) error {
	_, err := s.Execute(agentReviewer, "message.post", reviewMessageID, model.MessagePosted{
		Kind: "FYI", To: []string{agentTester}, Subject: "Auth/session release coordination",
		Body: "Flagging the auth/session release for your visibility once you're back online.",
	})
	return err
}
