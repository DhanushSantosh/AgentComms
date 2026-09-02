package app

import (
	"strings"
	"testing"
)

func TestApprovalBoundInvocationRejectsRelativeDeadline(t *testing.T) {
	command := (&cli{}).invocationCmd()
	command.SetArgs([]string{"request", "--id", "inv-1", "--to", "builder", "--instruction", "review", "--request-approval", "--expires-in", "1h"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "absolute --deadline") {
		t.Fatalf("expected actionable absolute-deadline error, got %v", err)
	}
}
