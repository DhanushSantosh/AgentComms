package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func invocationStatus(status string) cliui.Status {
	switch status {
	case "COMPLETED":
		return cliui.StatusSuccess
	case "REJECTED", "EXPIRED", "CANCELLED":
		return cliui.StatusDanger
	case "WAITING":
		return cliui.StatusWarning
	default:
		return cliui.StatusInfo
	}
}

func (c *cli) invocationCmd() *cobra.Command {
	root := &cobra.Command{Use: "invocation", Short: "Request and process agent invocations"}
	var target, messageID, taskID, instruction, expectedResult, priority, deadlineAt string
	var consumerMode, preferredRuntimeID string
	var invocationScopes []string
	var expiresIn time.Duration
	var requestApproval bool
	var approvalID, approvalReason, approvalTier string
	var approvalExpiresIn time.Duration
	request := &cobra.Command{Use: "request", Short: "Request an agent invocation", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			id = fmt.Sprintf("inv-%d", time.Now().UnixNano())
		}
		var deadline *time.Time
		if deadlineAt != "" && expiresIn > 0 {
			return errors.New("use either --deadline or --expires-in, not both")
		}
		if deadlineAt != "" {
			value, parseErr := time.Parse(time.RFC3339, deadlineAt)
			if parseErr != nil {
				return fmt.Errorf("deadline must be RFC3339: %w", parseErr)
			}
			value = value.UTC()
			deadline = &value
		} else if expiresIn > 0 {
			if requestApproval {
				return errors.New("approval-bound invocations require an absolute --deadline; relative --expires-in changes when the command is rerun")
			}
			value := time.Now().UTC().Add(expiresIn)
			deadline = &value
		}
		payload := model.InvocationRequested{
			Target: target, MessageID: messageID, TaskID: taskID, Instruction: instruction,
			ExpectedResult: expectedResult, Scopes: invocationScopes, Priority: priority,
			ConsumerMode:       model.ConsumerMode(strings.ToUpper(consumerMode)),
			PreferredRuntimeID: preferredRuntimeID, Deadline: deadline,
		}
		if requestApproval {
			if approvalID == "" {
				approvalID = "approval-invocation-" + id
			}
			if approvalExpiresIn <= 0 {
				approvalExpiresIn = 24 * time.Hour
			}
			event, err := c.svc.RequestApprovalForOperation(c.actor, approvalID, approvalTier, "invocation.request", id, payload, approvalReason, approvalExpiresIn)
			if err != nil {
				return err
			}
			return c.emit("approval.request", event)
		}
		event, err := c.svc.Execute(c.actor, "invocation.request", id, payload)
		if err != nil {
			return err
		}
		outcome, outcomeErr := c.invocationDeliveryOutcome(id, "")
		warnings := []string{}
		if outcomeErr != nil {
			warnings = append(warnings, "invocation was recorded, but delivery state could not be read: "+outcomeErr.Error())
		} else if outcome.Outcome == "UNAVAILABLE" || outcome.Outcome == "AMBIGUOUS" {
			warnings = append(warnings, "invocation was recorded, but no compatible delivery transport completed")
		}
		if c.json {
			return c.emitWithDelivery("invocation.request", event, outcome, warnings...)
		}
		status := cliui.StatusSuccess
		if outcome.Outcome == "UNAVAILABLE" || outcome.Outcome == "AMBIGUOUS" || outcomeErr != nil {
			status = cliui.StatusWarning
		}
		delivery := outcome.Outcome
		if delivery == "" {
			delivery = "recorded; delivery state pending"
		}
		return c.emitDocument("invocation.request", event, cliui.Document{
			Title:  "Invocation requested",
			Status: status,
			Fields: []cliui.Field{
				{Label: "Invocation", Value: id},
				{Label: "Target", Value: target},
				{Label: "Priority", Value: priority},
				{Label: "Consumer", Value: consumerMode},
				{Label: "Delivery", Value: delivery},
				{Label: "Runtime", Value: outcome.RuntimeID},
			},
			Hint: "Inspect the invocation to review delivery evidence and lifecycle state.",
		}, warnings...)
	}}
	request.Flags().String("id", "", "invocation ID (auto-generated if omitted)")
	request.Flags().StringVar(&target, "to", "", "target agent")
	_ = request.MarkFlagRequired("to")
	request.Flags().StringVar(&messageID, "message", "", "related message ID")
	request.Flags().StringVar(&taskID, "task", "", "related task ID")
	request.Flags().StringVar(&instruction, "instruction", "", "bounded instruction for the target agent")
	_ = request.MarkFlagRequired("instruction")
	request.Flags().StringVar(&expectedResult, "expected-result", "", "expected result")
	request.Flags().StringSliceVar(&invocationScopes, "scope", nil, "scope required by the invocation")
	request.Flags().StringVar(&priority, "priority", "NORMAL", "LOW, NORMAL, HIGH, or URGENT")
	request.Flags().StringVar(&consumerMode, "consumer", "", "INTERACTIVE_ONLY, WORKER_ONLY, or EITHER (target policy default when omitted)")
	request.Flags().StringVar(&preferredRuntimeID, "runtime", "", "specific target runtime")
	request.Flags().DurationVar(&expiresIn, "expires-in", 0, "deadline relative to now")
	request.Flags().StringVar(&deadlineAt, "deadline", "", "absolute RFC3339 deadline (recommended for approval-bound invocations)")
	request.Flags().BoolVar(&requestApproval, "request-approval", false, "request a payload-bound approval instead of invoking")
	request.Flags().StringVar(&approvalID, "approval-id", "", "approval ID (generated from the invocation ID when omitted)")
	request.Flags().StringVar(&approvalReason, "approval-reason", "", "reason shown to the approver")
	request.Flags().StringVar(&approvalTier, "approval-tier", "ORCHESTRATOR", "ORCHESTRATOR or HUMAN")
	request.Flags().DurationVar(&approvalExpiresIn, "approval-expires-in", 24*time.Hour, "approval validity window")

	list := &cobra.Command{Use: "list", Short: "List invocations", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		state, err := c.svc.State()
		if err != nil {
			return err
		}
		status, _ := cmd.Flags().GetString("status")
		targetFilter, _ := cmd.Flags().GetString("to")
		result := map[string]model.Invocation{}
		for id, invocation := range state.Invocations {
			if status != "" && invocation.Status != strings.ToUpper(status) {
				continue
			}
			if targetFilter != "" && invocation.Target != targetFilter {
				continue
			}
			result[id] = invocation
		}
		headers := []string{"ID", "TARGET", "STATUS", "PRIORITY", "REQUESTED_BY"}
		rows := make([][]string, 0, len(result))
		for _, id := range service.SortedKeys(result) {
			inv := result[id]
			rows = append(rows, []string{id, inv.Target, inv.Status, inv.Priority, inv.RequestedBy})
		}
		return c.emitTable("invocation.list", result, headers, rows)
	}}
	list.Flags().String("status", "", "filter by status")
	list.Flags().String("to", "", "filter by target agent")

	inspect := &cobra.Command{Use: "inspect", Short: "Show an invocation and its delivery evidence", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		state, err := c.svc.State()
		if err != nil {
			return err
		}
		invocation, exists := state.Invocations[id]
		if !exists {
			return fmt.Errorf("invocation %s not found", id)
		}
		deliveries := make([]model.InvocationDelivery, 0)
		for _, delivery := range state.InvocationDeliveries {
			if delivery.InvocationID == id {
				deliveries = append(deliveries, delivery)
			}
		}
		sort.Slice(deliveries, func(left, right int) bool {
			return deliveries[left].Attempt < deliveries[right].Attempt
		})
		result := map[string]any{
			"invocation": deliveriesAcknowledged(invocation),
			"deliveries": deliveries,
		}
		return c.emitDocument("invocation.inspect", result, cliui.Document{
			Title:  "Invocation " + invocation.ID,
			Status: invocationStatus(invocation.Status),
			Fields: []cliui.Field{
				{Label: "Status", Value: invocation.Status},
				{Label: "Target", Value: invocation.Target},
				{Label: "Requested by", Value: invocation.RequestedBy},
				{Label: "Priority", Value: invocation.Priority},
				{Label: "Runtime", Value: invocation.RuntimeID},
				{Label: "Deliveries", Value: fmt.Sprint(len(deliveries))},
			},
		})
	}}
	inspect.Flags().String("id", "", "invocation ID")
	_ = inspect.MarkFlagRequired("id")

	next := &cobra.Command{Use: "next", Short: "Show the next invocation available to a runtime", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		runtimeID, _ := cmd.Flags().GetString("runtime")
		invocation, found, err := c.svc.NextInvocation(c.actor, runtimeID)
		if err != nil {
			return err
		}
		result := map[string]any{"found": found, "invocation": invocation}
		if !found {
			return c.emitDocument("invocation.next", result, cliui.Document{Title: "No invocation available", Status: cliui.StatusInfo, Fields: []cliui.Field{{Label: "Runtime", Value: runtimeID}}})
		}
		return c.emitDocument("invocation.next", result, cliui.Document{
			Title: "Invocation available", Status: cliui.StatusInfo,
			Fields: []cliui.Field{{Label: "Invocation", Value: invocation.ID}, {Label: "Target", Value: invocation.Target}, {Label: "Priority", Value: invocation.Priority}, {Label: "Status", Value: invocation.Status}},
			Hint:   "Claim the invocation with the selected runtime before beginning work.",
		})
	}}
	next.Flags().String("runtime", "", "runtime ID used for capacity filtering")

	var redeliveryRuntimeID string
	redeliver := &cobra.Command{Use: "redeliver", Short: "Manually re-attempt delivery of an open invocation", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		state, err := c.svc.State()
		if err != nil {
			return err
		}
		invocation, ok := state.Invocations[id]
		if !ok {
			return fmt.Errorf("invocation %s not found", id)
		}
		if invocation.Status != "PENDING" && invocation.Status != "NOTIFIED" {
			return fmt.Errorf("invocation %s is %s, not open for redelivery", id, invocation.Status)
		}
		runtimeState, runtimeExists := state.AgentRuntimes[redeliveryRuntimeID]
		if !runtimeExists || runtimeState.AgentID != invocation.Target {
			return errors.New("redelivery runtime must be registered to the invocation target")
		}
		deliveryID := uuid.NewString()
		event, executeErr := c.svc.Execute(c.actor, "invocation.delivery-attempt", id,
			model.InvocationDeliveryAttempted{
				DeliveryID: deliveryID, RuntimeID: redeliveryRuntimeID,
				Transport: runtimeState.Connector, HostID: runtimeState.HostID, Manual: true,
			})
		if executeErr != nil {
			return executeErr
		}
		outcome, outcomeErr := c.invocationDeliveryOutcome(id, deliveryID)
		if outcomeErr != nil {
			return outcomeErr
		}
		if outcome.Outcome != "SUCCEEDED" {
			return &controlplane.Error{
				Code:    controlplane.CodeUnavailable,
				Message: "redelivery did not complete: " + outcome.Error,
			}
		}
		return c.emitWithDelivery("invocation.redeliver", event, outcome)
	}}
	redeliver.Flags().String("id", "", "open invocation ID to redeliver")
	_ = redeliver.MarkFlagRequired("id")
	redeliver.Flags().StringVar(&redeliveryRuntimeID, "runtime", "", "specific eligible target runtime")
	_ = redeliver.MarkFlagRequired("runtime")

	var runtimeID string
	var listenDuration time.Duration
	var autoClaim bool
	listen := &cobra.Command{Use: "listen", Short: "Block until an invocation is delivered, optionally claiming it", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		invocation, found, err := c.svc.ListenInvocation(c.actor, runtimeID, listenDuration)
		if err != nil {
			return err
		}
		result := map[string]any{"found": found, "invocation": invocation, "claimed": false}
		if found && autoClaim {
			event, claimErr := c.svc.Execute(c.actor, "invocation.claim", invocation.ID,
				model.InvocationClaimed{RuntimeID: runtimeID})
			if claimErr != nil {
				return claimErr
			}
			result["claimed"] = true
			result["claim_event"] = event
		}
		if c.jsonl {
			return c.emitStream("invocation.listen", "invocation.received", result)
		}
		if !found {
			return c.emitDocument("invocation.listen", result, cliui.Document{Title: "Listen window completed", Status: cliui.StatusInfo, Fields: []cliui.Field{{Label: "Invocation found", Value: "no"}, {Label: "Runtime", Value: runtimeID}}})
		}
		return c.emitDocument("invocation.listen", result, cliui.Document{
			Title: "Invocation received", Status: cliui.StatusSuccess,
			Fields: []cliui.Field{{Label: "Invocation", Value: invocation.ID}, {Label: "Target", Value: invocation.Target}, {Label: "Claimed", Value: fmt.Sprint(result["claimed"])}, {Label: "Runtime", Value: runtimeID}},
		})
	}}
	listen.Flags().StringVar(&runtimeID, "runtime", "", "connected runtime ID")
	_ = listen.MarkFlagRequired("runtime")
	listen.Flags().DurationVar(&listenDuration, "wait", controlplane.MaxInvocationListen, "bounded listen duration")
	listen.Flags().BoolVar(&autoClaim, "claim", true, "atomically claim a delivered invocation")

	claim := payloadStatus(c, "invocation", "claim", func(string) any {
		return model.InvocationClaimed{RuntimeID: runtimeID}
	})
	claim.Flags().StringVar(&runtimeID, "runtime", "", "claiming runtime ID")
	_ = claim.MarkFlagRequired("runtime")
	var summary string
	start := payloadStatus(c, "invocation", "start", func(string) any { return model.InvocationProgress{Summary: summary} })
	start.Flags().StringVar(&summary, "summary", "", "progress summary")
	// RFC 0027 section 4: the CLI verb is `defer` (it reads as a sibling
	// of listen/next -- receive work -- when named `wait`, but it means
	// "the worker is blocked, retry later"). The durable event type stays
	// `invocation.wait`.
	var waitReason string
	var retryIn time.Duration
	deferCommand := &cobra.Command{
		Use:   "defer",
		Short: "Mark a claimed invocation as blocked and due for a later retry",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetString("id")
			var nextAttempt *time.Time
			if retryIn > 0 {
				value := time.Now().UTC().Add(retryIn)
				nextAttempt = &value
			}
			v, e := c.svc.Execute(c.actor, "invocation.wait", id, model.InvocationWaiting{Reason: waitReason, NextAttemptAt: nextAttempt})
			if e != nil {
				return e
			}
			return c.emit("invocation.wait", v)
		},
	}
	deferCommand.Flags().String("id", "", "invocation ID")
	_ = deferCommand.MarkFlagRequired("id")
	deferCommand.Flags().StringVar(&waitReason, "reason", "", "why the invocation is blocked")
	_ = deferCommand.MarkFlagRequired("reason")
	deferCommand.Flags().DurationVar(&retryIn, "retry-in", 0, "next attempt relative to now")
	resume := payloadStatus(c, "invocation", "resume", func(string) any { return model.InvocationProgress{Summary: summary} })
	resume.Flags().StringVar(&summary, "summary", "", "progress summary")
	var resultMessage string
	complete := payloadStatus(c, "invocation", "complete", func(string) any {
		return model.InvocationCompleted{ResultMessageID: resultMessage, Summary: summary}
	})
	complete.Flags().StringVar(&resultMessage, "result-message", "", "result message ID")
	complete.Flags().StringVar(&summary, "summary", "", "completion summary")
	_ = complete.MarkFlagRequired("summary")
	var reason string
	reject := payloadStatus(c, "invocation", "reject", func(string) any { return model.InvocationRejected{Reason: reason} })
	reject.Flags().StringVar(&reason, "reason", "", "rejection reason")
	_ = reject.MarkFlagRequired("reason")
	expire := payloadStatus(c, "invocation", "expire", func(string) any { return model.InvocationRejected{Reason: reason} })
	expire.Flags().StringVar(&reason, "reason", "", "expiry reason")
	_ = expire.MarkFlagRequired("reason")
	cancelInvocation := payloadStatus(c, "invocation", "cancel", func(string) any { return model.InvocationRejected{Reason: reason} })
	cancelInvocation.Flags().StringVar(&reason, "reason", "", "cancellation reason")
	_ = cancelInvocation.MarkFlagRequired("reason")

	policy := c.invocationPolicyCmd()
	root.AddCommand(request, list, inspect, next, redeliver, listen, claim, start, deferCommand, resume, complete, reject, expire, cancelInvocation, policy)
	return root
}

func deliveriesAcknowledged(invocation model.Invocation) map[string]any {
	return map[string]any{
		"state":               invocation,
		"target_acknowledged": invocation.ClaimedAt != nil,
		"acknowledged_at":     invocation.ClaimedAt,
	}
}

func (c *cli) invocationPolicyCmd() *cobra.Command {
	root := &cobra.Command{Use: "policy", Short: "Manage per-agent invocation policy"}
	var mode string
	var trustedActors, allowedScopes []string
	var defaultConsumer, preferredInteractiveRuntime string
	var allowedConsumers []string
	var requireHuman bool
	set := &cobra.Command{Use: "set", Short: "Set a per-agent invocation policy", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		agentID, _ := cmd.Flags().GetString("agent")
		event, err := c.svc.Execute(c.actor, "invocation.policy.update", agentID, model.InvocationPolicyUpdated{
			Mode: mode, TrustedActors: trustedActors, AllowedScopes: allowedScopes,
			DefaultConsumerMode:           model.ConsumerMode(strings.ToUpper(defaultConsumer)),
			AllowedConsumerModes:          consumerModes(allowedConsumers),
			PreferredInteractiveRuntimeID: preferredInteractiveRuntime,
			RequireHumanForSensitive:      requireHuman,
		})
		if err != nil {
			return err
		}
		return c.emit("invocation.policy.update", event)
	}}
	set.Flags().String("agent", "", "target agent")
	_ = set.MarkFlagRequired("agent")
	set.Flags().StringVar(&mode, "mode", "MANUAL", "MANUAL, TRUSTED, AUTOMATIC, or DISABLED")
	set.Flags().StringSliceVar(&trustedActors, "trusted-actor", nil, "actor allowed by TRUSTED mode")
	set.Flags().StringSliceVar(&allowedScopes, "scope", nil, "allowed invocation scope")
	set.Flags().StringVar(&defaultConsumer, "default-consumer", "EITHER", "INTERACTIVE_ONLY, WORKER_ONLY, or EITHER")
	set.Flags().StringSliceVar(&allowedConsumers, "allow-consumer", nil, "allowed consumer mode (repeatable; defaults to all)")
	set.Flags().StringVar(&preferredInteractiveRuntime, "interactive-runtime", "", "preferred interactive runtime ID")
	set.Flags().BoolVar(&requireHuman, "require-human-for-sensitive", true, "require human approval for sensitive work")
	show := &cobra.Command{Use: "show", Short: "Show a per-agent invocation policy", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		agentID, _ := cmd.Flags().GetString("agent")
		state, err := c.svc.State()
		if err != nil {
			return err
		}
		policy := state.InvocationPolicies[agentID]
		return c.emitDocument("invocation.policy.show", policy, cliui.Document{
			Title: "Invocation policy", Status: cliui.StatusInfo,
			Fields: []cliui.Field{{Label: "Agent", Value: policy.AgentID}, {Label: "Mode", Value: policy.Mode}, {Label: "Default consumer", Value: string(policy.DefaultConsumerMode)}, {Label: "Preferred runtime", Value: policy.PreferredInteractiveRuntimeID}, {Label: "Updated by", Value: policy.UpdatedBy}},
		})
	}}
	show.Flags().String("agent", "", "target agent")
	_ = show.MarkFlagRequired("agent")
	root.AddCommand(set, show)
	return root
}

// gitBranch best-effort detects path's current git branch for `task lock`'s
// auto-filled Branch field -- task.create requires a non-empty branch, and
// an ad hoc lock has no natural one to ask the caller for. Returns "" on
// any failure (not a git repo, git not on PATH, detached HEAD reporting
// "HEAD" is passed through as-is rather than treated as a failure, since
// it's still a real, if unusual, answer).
func gitBranch(path string) string {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func consumerModes(values []string) []model.ConsumerMode {
	result := make([]model.ConsumerMode, 0, len(values))
	for _, value := range values {
		result = append(result, model.ConsumerMode(strings.ToUpper(strings.TrimSpace(value))))
	}
	return result
}

func (c *cli) sessionCmd() *cobra.Command {
	root := &cobra.Command{Use: "session", Short: "Manage durable invocation sessions"}
	shorts := map[string]string{
		"start": "Open a durable invocation session for this agent",
		"end":   "Close a durable invocation session",
	}
	for _, sub := range []string{"start", "end"} {
		sub := sub
		cmd := &cobra.Command{Use: sub, Short: shorts[sub], RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetString("id")
			v, e := c.svc.Execute(c.actor, "session."+sub, id, model.SessionPayload{AgentID: c.actor, PID: os.Getpid()})
			if e != nil {
				return e
			}
			return c.emit("session."+sub, v)
		}}
		cmd.Flags().String("id", "", "session ID")
		_ = cmd.MarkFlagRequired("id")
		root.AddCommand(cmd)
	}
	return root
}
