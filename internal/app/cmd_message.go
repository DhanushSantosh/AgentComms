package app

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/model"
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
		out := map[string]model.Message{}
		for id, m := range st.Messages {
			if unread && (m.Status != "OPEN" && m.Status != "DELIVERED") {
				continue
			}
			if from != "" && m.From != from {
				continue
			}
			for _, to := range m.To {
				if to == c.actor {
					out[id] = m
					break
				}
			}
		}
		if limit > 0 && len(out) > limit {
			trimmed := map[string]model.Message{}
			n := 0
			for id, m := range out {
				if n >= limit {
					break
				}
				trimmed[id] = m
				n++
			}
			out = trimmed
		}
		ids := make([]string, 0, len(out))
		for id := range out {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		rows := make([][]string, 0, len(ids))
		for _, id := range ids {
			message := out[id]
			rows = append(rows, []string{id, message.Kind, message.From, message.Status, message.Subject})
		}
		return c.emitTable("message.inbox", out, []string{"ID", "KIND", "FROM", "STATUS", "SUBJECT"}, rows)
	}}
	inbox.Flags().Bool("unread", false, "show only unread messages")
	inbox.Flags().String("from", "", "filter by sender")
	inbox.Flags().Int("limit", 0, "max results (0 = unlimited)")
	root.AddCommand(post, inbox)
	return root
}
func (c *cli) decisionCmd() *cobra.Command {
	root := &cobra.Command{Use: "decision"}
	for _, sub := range []string{"create", "supersede"} {
		sub := sub
		var title, statement, supersedes string
		var to []string
		shortBySub := map[string]string{"create": "Record a durable decision", "supersede": "Replace a prior decision with a new one"}
		cmd := &cobra.Command{Use: sub, Short: shortBySub[sub], RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetString("id")
			if sub == "create" && strings.TrimSpace(id) == "" {
				id = fmt.Sprintf("decision-%d", time.Now().UnixNano())
			}
			v, e := c.svc.Execute(c.actor, "decision."+sub, id, model.DecisionPayload{Title: title, Statement: statement, Supersedes: supersedes, To: to})
			if e != nil {
				return e
			}
			return c.emit("decision."+sub, v)
		}}
		if sub == "create" {
			cmd.Flags().String("id", "", "decision ID (auto-generated if omitted)")
		} else {
			cmd.Flags().String("id", "", "decision ID")
			_ = cmd.MarkFlagRequired("id")
		}
		cmd.Flags().StringVar(&title, "title", "", "title")
		cmd.Flags().StringVar(&statement, "statement", "", "statement")
		cmd.Flags().StringVar(&supersedes, "supersedes", "", "prior decision")
		cmd.Flags().StringSliceVar(&to, "to", nil, "acknowledging principal")
		root.AddCommand(cmd)
	}
	root.AddCommand(c.entityShow("decision", func(st model.State, id string) (any, []cliui.Field, bool) {
		d, ok := st.Decisions[id]
		if !ok {
			return nil, nil, false
		}
		return d, []cliui.Field{
			{Label: "Title", Value: d.Title}, {Label: "Statement", Value: d.Statement},
			{Label: "Status", Value: d.Status}, {Label: "Supersedes", Value: d.Supersedes},
		}, true
	}))
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
		ids := make([]string, 0, len(st.Approvals))
		for id := range st.Approvals {
			ids = append(ids, id)
		}
		sort.Strings(ids)
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
		return a, []cliui.Field{
			{Label: "Tier", Value: a.Tier}, {Label: "Status", Value: a.Status},
			{Label: "Requester", Value: a.Requester}, {Label: "Action", Value: a.Action},
			{Label: "Reason", Value: a.Reason},
		}, true
	})
	root.AddCommand(request, approve, reject, list, show)
	return root
}
