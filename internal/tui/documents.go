package tui

import (
	"errors"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/table"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

var documentCreateForm = &ActionForm{
	Title: "Create document",
	Hint:  "Living documents are versioned; use update to publish a new version in place.",
	Fields: []FormField{
		{Label: "Document ID", Placeholder: "guide-v1", Required: true},
		{Label: "Title", Placeholder: "Operator guide", Required: true},
		{Label: "Body", Placeholder: "", Required: true},
		{Label: "Tags (comma-separated)", Placeholder: "reference"},
	},
	Build: func(v []string) (any, error) {
		if strings.TrimSpace(v[2]) == "" {
			return nil, errors.New("body is required")
		}
		return model.DocumentPayload{Title: v[1], Body: v[2], Tags: splitCSV(v[3])}, nil
	},
	ResolveID: func(_ string, v []string) string { return v[0] },
}

var documentUpdateForm = &ActionForm{
	Title: "Update document",
	Hint:  "Publishes a new version of this document in place.",
	Fields: []FormField{
		{Label: "Title", Placeholder: "", Required: true},
		{Label: "Body", Placeholder: "", Required: true},
		{Label: "Tags (comma-separated)", Placeholder: ""},
	},
	Build: func(v []string) (any, error) {
		if strings.TrimSpace(v[1]) == "" {
			return nil, errors.New("body is required")
		}
		return model.DocumentPayload{Title: v[0], Body: v[1], Tags: splitCSV(v[2])}, nil
	},
}

var documentSupersedeForm = &ActionForm{
	Title: "Supersede document",
	Hint:  "The replacement document must already exist; create it first if needed.",
	Fields: []FormField{
		{Label: "Replacement document ID", Placeholder: "guide-v2", Required: true},
	},
	Build: func(v []string) (any, error) {
		return model.DocumentPayload{ReplacementID: v[0]}, nil
	},
}

var (
	docUpdate    = RowAction{Key: "e", Label: "update", EventType: "document.update", Form: documentUpdateForm}
	docSupersede = RowAction{Key: "s", Label: "supersede", EventType: "document.supersede", Form: documentSupersedeForm}
)

type documentRowSource struct{}

func (documentRowSource) Columns(width int) []table.Column {
	status, version, author := 12, 8, 13
	id := width - status - version - author
	if id < 14 {
		id = 14
	}
	return []table.Column{
		{Title: "STATUS", Width: status},
		{Title: "DOCUMENT", Width: id},
		{Title: "VERSION", Width: version},
		{Title: "AUTHOR", Width: author},
	}
}
func (documentRowSource) Rows(st model.State, actor string, mine bool) []table.Row {
	ids := service.SortedKeys(st.Documents)
	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		d := st.Documents[id]
		rows = append(rows, table.Row{d.Status, id, strconv.Itoa(d.Version), d.Author})
	}
	return rows
}
func (documentRowSource) RowID(idx int, st model.State, actor string, mine bool) string {
	ids := service.SortedKeys(st.Documents)
	if idx < 0 || idx >= len(ids) {
		return ""
	}
	return ids[idx]
}

// documentActionsFor offers update/supersede on any document -- neither
// transition is role-gated beyond the ordinary active-principal write check
// (internal/protocol/transitions.go has no elevation requirement for
// document.*), and both remain valid regardless of the document's current
// status (a SUPERSEDED document can still be corrected via update, matching
// what the CLI already permits).
func (documentRowSource) Actions(id string, st model.State, actor string) []RowAction {
	if _, ok := st.Documents[id]; !ok {
		return nil
	}
	return []RowAction{docUpdate, docSupersede}
}
