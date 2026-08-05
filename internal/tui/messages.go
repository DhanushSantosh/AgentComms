package tui

import (
	"strings"

	"charm.land/bubbles/v2/table"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

var messagePostForm = &ActionForm{
	Title: "Post message",
	Hint:  "FYI delivers without acknowledgement; ACTION/CONTRACT/BLOCKER/DECISION create typed obligations for each recipient.",
	Fields: []FormField{
		{Label: "Message ID", Placeholder: "action-001", Required: true},
		{Label: "Kind (FYI/ACTION/CONTRACT/BLOCKER/DECISION)", Placeholder: "FYI"},
		{Label: "To (comma-separated)", Placeholder: "builder,reviewer", Required: true},
		{Label: "Subject", Placeholder: "Run integration tests", Required: true},
		{Label: "Body", Placeholder: ""},
		{Label: "Related task ID", Placeholder: ""},
	},
	Build: func(v []string) (any, error) {
		kind := strings.ToUpper(strings.TrimSpace(v[1]))
		if kind == "" {
			kind = "FYI"
		}
		return model.MessagePosted{Kind: kind, To: splitCSV(v[2]), Subject: v[3], Body: v[4], TaskID: v[5]}, nil
	},
	ResolveID: func(_ string, v []string) string { return v[0] },
	ConfirmIf: func(payload any) (bool, string) {
		p := payload.(model.MessagePosted)
		if p.Kind == "CONTRACT" {
			return true, "Publish a CONTRACT to " + strings.Join(p.To, ", ") + "? All named parties must accept before it is satisfied."
		}
		return false, ""
	},
}

var (
	msgAck      = RowAction{Key: "a", Label: "ack", EventType: "message.ack", Payload: func() any { return model.MessageResponse{} }}
	msgReject   = RowAction{Key: "x", Label: "reject", EventType: "message.reject", Payload: func() any { return model.MessageResponse{} }}
	msgComplete = RowAction{Key: "p", Label: "complete", EventType: "message.complete", Payload: func() any { return model.MessageResponse{} }}
	msgResolve  = RowAction{Key: "v", Label: "resolve", EventType: "message.resolve", Payload: func() any { return model.MessageResponse{} }}
)

// messageActionsFor mirrors service.go's message.ack/reject/complete/resolve
// rules: only a listed recipient gets any action; FYI never needs a response
// (it is delivered, not obligated); reject has no documented recipient
// workflow for DECISION (governance.md: "DECISION requires read
// acknowledgement" only); complete/resolve are curated to appear once the
// recipient has first acknowledged, even though the service itself does not
// require that ordering.
func messageActionsFor(m model.Message, actor string) []RowAction {
	if m.Kind == "FYI" {
		return nil
	}
	var mine *model.RecipientState
	for i := range m.Recipients {
		if m.Recipients[i].Principal == actor {
			mine = &m.Recipients[i]
			break
		}
	}
	if mine == nil {
		return nil
	}
	switch mine.Status {
	case "PENDING":
		acts := []RowAction{msgAck}
		if m.Kind != "DECISION" {
			acts = append(acts, msgReject)
		}
		return acts
	case "ACCEPTED":
		if m.Kind == "ACTION" {
			return []RowAction{msgComplete}
		}
	case "ACKNOWLEDGED":
		if m.Kind == "BLOCKER" {
			return []RowAction{msgResolve}
		}
	}
	return nil
}

// messageRowSource.owner is the real project owner's principal ID
// (store.Config().Owner), never the literal string "owner" -- a project's
// owner can be registered under any ID (e.g. "Dhanush"), and comparing the
// viewing actor against a hardcoded "owner" would silently never grant the
// owner visibility into every message on a real project.
type messageRowSource struct{ owner string }

func (messageRowSource) Columns(width int) []table.Column {
	kind, from, state := 10, 13, 12
	if width < 75 {
		kind, from, state = 8, 10, 10
	}
	subj := max(8, width-kind-from-state)
	return []table.Column{
		{Title: "KIND", Width: kind},
		{Title: "FROM", Width: from},
		{Title: "SUBJECT", Width: subj},
		{Title: "STATE", Width: state},
	}
}
func (s messageRowSource) filteredIDs(st model.State, actor string) []string {
	ids := make([]string, 0, len(st.Messages))
	for _, id := range service.SortedKeys(st.Messages) {
		m := st.Messages[id]
		addressed := s.owner != "" && actor == s.owner
		for _, to := range m.To {
			if to == actor {
				addressed = true
			}
		}
		if addressed {
			ids = append(ids, id)
		}
	}
	return ids
}
func recipientStatus(m model.Message, actor string) string {
	for _, r := range m.Recipients {
		if r.Principal == actor {
			return r.Status
		}
	}
	return m.Status
}
func (s messageRowSource) Rows(st model.State, actor string, mine bool) []table.Row {
	ids := s.filteredIDs(st, actor)
	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		m := st.Messages[id]
		rows = append(rows, table.Row{m.Kind, m.From, m.Subject, recipientStatus(m, actor)})
	}
	return rows
}
func (s messageRowSource) RowID(idx int, st model.State, actor string, mine bool) string {
	ids := s.filteredIDs(st, actor)
	if idx < 0 || idx >= len(ids) {
		return ""
	}
	return ids[idx]
}
func (messageRowSource) Actions(id string, st model.State, actor string) []RowAction {
	m, ok := st.Messages[id]
	if !ok {
		return nil
	}
	return messageActionsFor(m, actor)
}
