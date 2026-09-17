// Package claudetail renders one Claude Code transcript line as a
// human-readable turn. Historically this package also tailed a Claude
// Code session's transcript file directly off disk (RFC 0008); RFC 0034
// removed that file-watching path in favor of `live attach`'s
// broker-subscription model, which is what this package's sole remaining
// export, Format, now serves exclusively.
package claudetail

import (
	"encoding/json"
	"fmt"
	"strings"
)

type transcriptEntry struct {
	Type    string `json:"type"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Name string `json:"name"`
}

// Format renders one JSONL transcript line as a human-readable turn, or
// returns ok=false for a line with nothing worth showing: attachments,
// internal bookkeeping markers (bridge-session, last-prompt), and anything
// this parser doesn't recognize. Best-effort by design, the same way
// loadOpenCodeLiveSessionID treats a malformed cache entry as absent rather
// than an error -- this is Claude Code's internal transcript format, not a
// documented, stable contract.
func Format(line []byte) (rendered string, ok bool) {
	var entry transcriptEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return "", false
	}
	if entry.Type != "user" && entry.Type != "assistant" {
		return "", false
	}

	var text string
	if err := json.Unmarshal(entry.Message.Content, &text); err == nil {
		text = strings.TrimSpace(text)
	} else {
		var blocks []contentBlock
		if err := json.Unmarshal(entry.Message.Content, &blocks); err != nil {
			return "", false
		}
		var body strings.Builder
		for _, block := range blocks {
			switch block.Type {
			case "text":
				if trimmed := strings.TrimSpace(block.Text); trimmed != "" {
					body.WriteString(trimmed)
					body.WriteString("\n")
				}
			case "tool_use":
				fmt.Fprintf(&body, "[tool call: %s]\n", block.Name)
			}
		}
		text = strings.TrimRight(body.String(), "\n")
	}
	if text == "" {
		return "", false
	}

	label := "USER"
	if entry.Type == "assistant" {
		label = "ASSISTANT"
	}
	return fmt.Sprintf("--- %s ---\n%s\n", label, text), true
}
