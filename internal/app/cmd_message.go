package app

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/spf13/cobra"
)

func (c *cli) messageCmd() *cobra.Command {
	root := &cobra.Command{Use: "message"}
	var kind, subject, body, taskID, bodyFile string
	var to []string
	post := &cobra.Command{Use: "post", RunE: func(cmd *cobra.Command, args []string) error {
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
		v, e := c.svc.Execute(c.actor, "message.post", id, model.MessagePosted{Kind: strings.ToUpper(kind), To: to, Subject: subject, Body: body, TaskID: taskID})
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
	for _, sub := range []string{"ack", "reject", "complete", "resolve"} {
		postCmd := payloadStatus(c, "message", sub, func(string) any { return model.MessageResponse{} })
		root.AddCommand(postCmd)
	}
	inbox := &cobra.Command{Use: "inbox", RunE: func(cmd *cobra.Command, args []string) error {
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
		cmd := &cobra.Command{Use: sub, RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetString("id")
			v, e := c.svc.Execute(c.actor, "decision."+sub, id, model.DecisionPayload{Title: title, Statement: statement, Supersedes: supersedes, To: to})
			if e != nil {
				return e
			}
			return c.emit("decision."+sub, v)
		}}
		cmd.Flags().String("id", "", "decision ID")
		_ = cmd.MarkFlagRequired("id")
		cmd.Flags().StringVar(&title, "title", "", "title")
		cmd.Flags().StringVar(&statement, "statement", "", "statement")
		cmd.Flags().StringVar(&supersedes, "supersedes", "", "prior decision")
		cmd.Flags().StringSliceVar(&to, "to", nil, "acknowledging principal")
		root.AddCommand(cmd)
	}
	return root
}
func (c *cli) approvalCmd() *cobra.Command {
	root := &cobra.Command{Use: "approval"}
	var tier, action, reason string
	var affected []string
	request := &cobra.Command{Use: "request", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		v, e := c.svc.Execute(c.actor, "approval.request", id, model.ApprovalRequested{Tier: strings.ToUpper(tier), Action: action, Reason: reason, Affected: affected})
		if e != nil {
			return e
		}
		return c.emit("approval.request", v)
	}}
	request.Flags().String("id", "", "approval ID")
	_ = request.MarkFlagRequired("id")
	request.Flags().StringVar(&tier, "tier", "ORCHESTRATOR", "ORCHESTRATOR or HUMAN")
	request.Flags().StringVar(&action, "action", "", "proposed action")
	request.Flags().StringVar(&reason, "reason", "", "reason")
	request.Flags().StringSliceVar(&affected, "affected", nil, "affected principal")
	approve := payloadStatus(c, "approval", "approve", func(string) any { return model.ApprovalResponse{} })
	reject := payloadStatus(c, "approval", "reject", func(string) any { return model.ApprovalResponse{} })
	list := &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
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
	root.AddCommand(request, approve, reject, list)
	return root
}
