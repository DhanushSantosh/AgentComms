package codexserve

import (
	"encoding/json"
	"strings"
)

type notificationEnvelope struct {
	Method string `json:"method"`
	Params struct {
		Item struct {
			Type    string `json:"type"`
			Phase   string `json:"phase"`
			Text    string `json:"text"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"item"`
	} `json:"params"`
}

// Format renders one raw JSON-RPC line from a Codex app-server process as
// a human-readable turn, the same "--- USER ---"/"--- ASSISTANT ---" style
// claudetail.Format already established, for a consistent operator
// experience across both attach commands. Codex's wire format is JSON-RPC
// notifications, not Claude's transcript-line shape, so this is its own
// parser rather than a forced reuse of claudetail's.
//
// Returns ok=false for anything that isn't a completed user or assistant
// message: request/response envelopes, deltas, tool-call bookkeeping, and
// anything this parser doesn't recognize -- best-effort, the same way
// claudetail.Format treats an unrecognized transcript line as absent
// rather than an error.
func Format(line []byte) (rendered string, ok bool) {
	var envelope notificationEnvelope
	if err := json.Unmarshal(line, &envelope); err != nil {
		return "", false
	}
	if envelope.Method != "item/completed" {
		return "", false
	}
	switch envelope.Params.Item.Type {
	case "userMessage":
		var body strings.Builder
		for _, block := range envelope.Params.Item.Content {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				body.WriteString(block.Text)
			}
		}
		if body.Len() == 0 {
			return "", false
		}
		return "--- USER ---\n" + body.String() + "\n", true
	case "agentMessage":
		if envelope.Params.Item.Phase != "final_answer" {
			return "", false
		}
		text := strings.TrimSpace(envelope.Params.Item.Text)
		if text == "" {
			return "", false
		}
		return "--- ASSISTANT ---\n" + text + "\n", true
	default:
		return "", false
	}
}
