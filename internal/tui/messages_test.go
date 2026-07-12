package tui

import (
	"reflect"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func msgLabels(acts []RowAction) []string {
	if len(acts) == 0 {
		return nil
	}
	out := make([]string, len(acts))
	for i, a := range acts {
		out[i] = a.Label
	}
	return out
}

func recipient(status string) []model.RecipientState {
	return []model.RecipientState{{Principal: "builder", Status: status}}
}

func TestMessageActionsForStates(t *testing.T) {
	cases := []struct {
		name string
		msg  model.Message
		want []string
	}{
		{"fyi never needs a response", model.Message{Kind: "FYI", Recipients: recipient("DELIVERED")}, nil},
		{"action pending offers ack and reject", model.Message{Kind: "ACTION", Recipients: recipient("PENDING")}, []string{"ack", "reject"}},
		{"contract pending offers ack and reject", model.Message{Kind: "CONTRACT", Recipients: recipient("PENDING")}, []string{"ack", "reject"}},
		{"blocker pending offers ack and reject", model.Message{Kind: "BLOCKER", Recipients: recipient("PENDING")}, []string{"ack", "reject"}},
		{"decision pending offers ack only", model.Message{Kind: "DECISION", Recipients: recipient("PENDING")}, []string{"ack"}},
		{"action accepted offers complete", model.Message{Kind: "ACTION", Recipients: recipient("ACCEPTED")}, []string{"complete"}},
		{"contract accepted offers nothing further", model.Message{Kind: "CONTRACT", Recipients: recipient("ACCEPTED")}, nil},
		{"blocker acknowledged offers resolve", model.Message{Kind: "BLOCKER", Recipients: recipient("ACKNOWLEDGED")}, []string{"resolve"}},
		{"decision acknowledged offers nothing further", model.Message{Kind: "DECISION", Recipients: recipient("ACKNOWLEDGED")}, nil},
		{"rejected is terminal", model.Message{Kind: "ACTION", Recipients: recipient("REJECTED")}, nil},
		{"completed is terminal", model.Message{Kind: "ACTION", Recipients: recipient("COMPLETED")}, nil},
		{"non-recipient sees nothing", model.Message{Kind: "ACTION", Recipients: []model.RecipientState{{Principal: "other", Status: "PENDING"}}}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := msgLabels(messageActionsFor(c.msg, "builder"))
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
