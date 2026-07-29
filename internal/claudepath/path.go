package claudepath

import (
	"path/filepath"
	"strings"
)

// SessionPath returns Claude Code's local JSONL transcript path for a
// project/session pair on the current operating system.
func SessionPath(claudeHome, projectDirectory, sessionID string) (string, error) {
	absoluteProject, err := filepath.Abs(projectDirectory)
	if err != nil {
		return "", err
	}
	return filepath.Join(
		claudeHome,
		"projects",
		ProjectSlug(absoluteProject),
		sessionID+".jsonl",
	), nil
}

// ProjectSlug converts an absolute project directory into Claude Code's
// filesystem-safe project key. Both separators and Windows volume colons map
// to dashes.
func ProjectSlug(absoluteProject string) string {
	normalized := filepath.ToSlash(filepath.Clean(absoluteProject))
	return strings.NewReplacer("/", "-", ":", "-").Replace(normalized)
}
