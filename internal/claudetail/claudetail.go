// Package claudetail streams a Claude Code session transcript live, the
// same role opencodeclient.Subscribe/opencode attach fills for OpenCode.
// Claude Code has no server or API for this: each session is just a local,
// append-only JSONL file, and nothing else watches it while the process
// that owns it is running. This package tails that file directly instead.
package claudetail

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// SessionPath returns the local file Claude Code appends this session's
// transcript to, confirmed by inspecting real session files on disk:
// <claudeHome>/projects/<projectDir, "/" replaced by "-">/<sessionID>.jsonl.
func SessionPath(claudeHome, projectDir, sessionID string) (string, error) {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return "", err
	}
	slug := strings.ReplaceAll(abs, "/", "-")
	return filepath.Join(claudeHome, "projects", slug, sessionID+".jsonl"), nil
}

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

// Tail streams a Claude Code session transcript to out as it grows:
// replays existing content once if replayHistory is set, then watches path
// for appended lines until ctx is canceled.
func Tail(ctx context.Context, path string, out io.Writer, replayHistory bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)
	if replayHistory {
		if err := drain(reader, out); err != nil {
			return err
		}
	} else if _, err := file.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Close() }()
	if err := watcher.Add(filepath.Dir(path)); err != nil {
		return err
	}

	// A periodic drain alongside the fsnotify watch guards against any
	// write that doesn't cleanly surface as a Write event for this exact
	// path (e.g. an editor's atomic rename-based save elsewhere in the same
	// directory triggering a directory-level event instead) -- it should
	// never be the only thing keeping this live, but it stops the tail from
	// silently stalling if fsnotify ever misses one.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Name != path || event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			if err := drain(reader, out); err != nil {
				return err
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			return err
		case <-ticker.C:
			if err := drain(reader, out); err != nil {
				return err
			}
		}
	}
}

func drain(reader *bufio.Reader, out io.Writer) error {
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if rendered, ok := Format(line); ok {
				fmt.Fprint(out, rendered)
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}
