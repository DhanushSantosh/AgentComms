package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/spf13/cobra"
)

func (c *cli) messageCmd() *cobra.Command {
	root := &cobra.Command{Use: "message"}
	var kind, subject, body, taskID, bodyFile string
	var requestApproval bool
	var approvalID, approvalReason string
	var approvalExpiresIn time.Duration
	var to []string
	post := &cobra.Command{Use: "post", Short: "Post a message to recipients", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			id = fmt.Sprintf("msg-%d", time.Now().UnixNano())
		}
		if bodyFile != "" {
			b, e := os.ReadFile(bodyFile)
			if e != nil {
				return e
			}
			body = string(b)
		}
		payload := model.MessagePosted{Kind: strings.ToUpper(kind), To: to, Subject: subject, Body: body, TaskID: taskID}
		if requestApproval {
			if payload.Kind != "CONTRACT" {
				return fmt.Errorf("--request-approval is only valid for CONTRACT messages")
			}
			if approvalID == "" {
				approvalID = "approval-contract-" + id
			}
			if approvalExpiresIn <= 0 {
				approvalExpiresIn = 24 * time.Hour
			}
			v, e := c.svc.RequestApprovalForOperation(c.actor, approvalID, "ORCHESTRATOR", "message.post", id, payload, approvalReason, approvalExpiresIn)
			if e != nil {
				return e
			}
			return c.emit("approval.request", v)
		}
		v, e := c.svc.Execute(c.actor, "message.post", id, payload)
		if e != nil {
			return e
		}
		return c.emit("message.post", v)
	}}
	post.Flags().String("id", "", "message ID (auto-generated if omitted)")
	post.Flags().StringVar(&kind, "kind", "FYI", "message kind (FYI, ACTION, CONTRACT, BLOCKER, DECISION)")
	post.Flags().StringSliceVar(&to, "to", nil, "recipient")
	post.Flags().StringVar(&subject, "subject", "", "subject")
	post.Flags().StringVar(&body, "body", "", "body")
	post.Flags().StringVar(&bodyFile, "body-file", "", "read body from file (bypasses CLI arg limits)")
	post.Flags().StringVar(&taskID, "task", "", "related task")
	post.Flags().BoolVar(&requestApproval, "request-approval", false, "request a payload-bound approval instead of posting")
	post.Flags().StringVar(&approvalID, "approval-id", "", "approval ID (generated from the message ID when omitted)")
	post.Flags().StringVar(&approvalReason, "approval-reason", "", "reason shown to the approver")
	post.Flags().DurationVar(&approvalExpiresIn, "approval-expires-in", 24*time.Hour, "approval validity window")
	for _, sub := range []string{"ack", "reject", "complete", "resolve"} {
		postCmd := payloadStatus(c, "message", sub, func(string) any { return model.MessageResponse{} })
		root.AddCommand(postCmd)
	}
	inbox := &cobra.Command{Use: "inbox", Short: "List messages addressed to you", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		unread, _ := cmd.Flags().GetBool("unread")
		from, _ := cmd.Flags().GetString("from")
		limit, _ := cmd.Flags().GetInt("limit")
		filtered := unread || from != ""
		addressedToActor := false
		out := map[string]model.Message{}
		for id, m := range st.Messages {
			toActor := false
			for _, to := range m.To {
				if to == c.actor {
					toActor = true
					break
				}
			}
			if !toActor {
				continue
			}
			addressedToActor = true
			// UX-05: unread must reflect *this* recipient's own obligation,
			// not the message's aggregate Status -- a two-recipient ACTION
			// stays "OPEN" (aggregate) until every recipient has responded,
			// so an already-acknowledged recipient kept seeing it as
			// unread here purely because someone else hadn't acted yet.
			// FYI has no obligation (initialRecipientStatus never sets it
			// to PENDING for FYI), so it correctly never matches --unread
			// either way -- no new durable read-state invented for it.
			if unread {
				pending := false
				for _, recipient := range m.Recipients {
					if recipient.Principal == c.actor && recipient.Status == "PENDING" {
						pending = true
						break
					}
				}
				if !pending {
					continue
				}
			}
			if from != "" && m.From != from {
				continue
			}
			out[id] = m
		}
		// UX-05: sort before limiting, not after -- trimming a Go map
		// (whose iteration order is randomized per-run) before sorting
		// meant an unchanged inbox returned different IDs across repeated
		// `--limit 1` calls, breaking any kind of stable pagination.
		ids := service.SortedKeys(out)
		if limit > 0 && len(ids) > limit {
			ids = ids[:limit]
			limited := make(map[string]model.Message, len(ids))
			for _, id := range ids {
				limited[id] = out[id]
			}
			out = limited
		}
		rows := make([][]string, 0, len(ids))
		for _, id := range ids {
			message := out[id]
			rows = append(rows, []string{id, message.Kind, message.From, message.Status, message.Subject})
		}
		// UX-04: SUBJECT and FROM are what a person actually reads this
		// list for; ID is the long machine identifier `message show --id`
		// needs, useful but the most acceptable to drop first when the
		// terminal is narrow. Column order (for muscle memory / --json
		// stability) is unchanged; only removal priority moves.
		headers := []string{"ID", "KIND", "FROM", "STATUS", "SUBJECT"}
		priorities := []int{4, 2, 0, 3, 1}
		// UX-15: "(no rows)" read identically whether nothing has ever been
		// addressed to this actor or --unread/--from just narrowed a real
		// inbox to zero -- distinguish the two and name the fix for the
		// filtered case (the unfiltered case has no fix to name; posting a
		// message is someone else's action, not this actor's).
		empty := "Nothing addressed to you yet."
		if filtered && addressedToActor {
			empty = "No messages match this filter. Remove --unread/--from to see everything addressed to you."
		}
		return c.emitTableFull("message.inbox", out, headers, priorities, empty, rows)
	}}
	inbox.Flags().Bool("unread", false, "show only unread messages")
	inbox.Flags().String("from", "", "filter by sender")
	inbox.Flags().Int("limit", 0, "max results (0 = unlimited)")
	// UX-04 / RFC 0032: every other domain RFC 0027 touched (task, agent,
	// approval, decision) got a uniform `show`; messages were missed. This
	// is the one place to read a message's subject and body directly
	// instead of already knowing to pass inbox --details or --json.
	show := c.entityShow("message", func(st model.State, id string) (any, []cliui.Field, bool) {
		m, ok := st.Messages[id]
		if !ok {
			return nil, nil, false
		}
		recipients := make([]string, 0, len(m.Recipients))
		for _, r := range m.Recipients {
			recipients = append(recipients, r.Principal+": "+r.Status)
		}
		return m, []cliui.Field{
			{Label: "Kind", Value: m.Kind}, {Label: "From", Value: m.From},
			{Label: "Status", Value: m.Status}, {Label: "Subject", Value: m.Subject},
			{Label: "Body", Value: m.Body}, {Label: "Recipients", Value: strings.Join(recipients, ", ")},
		}, true
	})
	root.AddCommand(post, inbox, show)
	return root
}
func (c *cli) approvalCmd() *cobra.Command {
	root := &cobra.Command{Use: "approval"}
	var tier, action, reason, subjectDigest, approvalSubject string
	var expiresIn time.Duration
	var affected []string
	request := &cobra.Command{Use: "request", Short: "Request a governed approval", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if strings.TrimSpace(id) == "" {
			id = fmt.Sprintf("approval-%d", time.Now().UnixNano())
		}
		var expiresAt *time.Time
		if expiresIn > 0 {
			value := time.Now().UTC().Add(expiresIn)
			expiresAt = &value
		}
		v, e := c.svc.Execute(c.actor, "approval.request", id, model.ApprovalRequested{Tier: strings.ToUpper(tier), Action: action, SubjectDigest: subjectDigest, Subject: approvalSubject, Reason: reason, Affected: affected, ExpiresAt: expiresAt})
		if e != nil {
			return e
		}
		return c.emit("approval.request", v)
	}}
	request.Flags().String("id", "", "approval ID (auto-generated if omitted)")
	request.Flags().StringVar(&tier, "tier", "ORCHESTRATOR", "ORCHESTRATOR or HUMAN")
	request.Flags().StringVar(&action, "action", "", "proposed action")
	request.Flags().StringVar(&reason, "reason", "", "reason")
	request.Flags().StringSliceVar(&affected, "affected", nil, "affected principal")
	request.Flags().StringVar(&subjectDigest, "subject-digest", "", "canonical operation digest for a bound approval")
	request.Flags().StringVar(&approvalSubject, "subject-json", "", "canonical operation JSON for approver review")
	request.Flags().DurationVar(&expiresIn, "expires-in", 0, "approval validity window")
	approve := payloadStatus(c, "approval", "approve", func(string) any { return model.ApprovalResponse{} })
	reject := payloadStatus(c, "approval", "reject", func(string) any { return model.ApprovalResponse{} })
	list := &cobra.Command{Use: "list", Short: "List approvals and their status", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		ids := service.SortedKeys(st.Approvals)
		rows := make([][]string, 0, len(ids))
		for _, id := range ids {
			approval := st.Approvals[id]
			rows = append(rows, []string{id, approval.Tier, approval.Status, approval.Requester, approval.Action})
		}
		return c.emitTable("approval.list", st.Approvals, []string{"ID", "TIER", "STATUS", "REQUESTER", "ACTION"}, rows)
	}}
	show := c.entityShow("approval", func(st model.State, id string) (any, []cliui.Field, bool) {
		a, ok := st.Approvals[id]
		if !ok {
			return nil, nil, false
		}
		// UX-06: the operation being reviewed, its expiry, and who it
		// affects were only available under --details, even though a
		// reviewer following the natural "show then approve" workflow is
		// exactly who needs that information -- it's what RFC 0025's bound-
		// approval design expects a reviewer to actually check, not
		// secondary metadata. The TUI's approval inspector already shows
		// expiry and subject by default; this brings the CLI's default
		// view to the same bar.
		fields := []cliui.Field{
			{Label: "Tier", Value: a.Tier}, {Label: "Status", Value: a.Status},
			{Label: "Requester", Value: a.Requester}, {Label: "Action", Value: a.Action},
		}
		if a.Subject != "" {
			fields = append(fields, cliui.Field{Label: "Subject", Value: a.Subject})
		}
		if len(a.Affected) > 0 {
			fields = append(fields, cliui.Field{Label: "Affected", Value: strings.Join(a.Affected, ", ")})
		}
		if a.ExpiresAt != nil {
			fields = append(fields, cliui.Field{Label: "Expires", Value: a.ExpiresAt.Format(time.RFC3339)})
		}
		fields = append(fields, cliui.Field{Label: "Reason", Value: a.Reason})
		return a, fields, true
	})
	root.AddCommand(request, approve, reject, list, show)
	return root
}
