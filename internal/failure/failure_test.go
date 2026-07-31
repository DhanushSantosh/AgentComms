package failure

import (
	"errors"
	"fmt"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
)

func TestCodePreservesControlPlaneErrors(t *testing.T) {
	err := &controlplane.Error{Code: controlplane.CodeConflict, Message: "conflict"}
	if got := Code(err); got != string(controlplane.CodeConflict) {
		t.Fatalf("unexpected code %q", got)
	}
	if got := ExitStatus(err); got != 9 {
		t.Fatalf("unexpected exit status %d", got)
	}
}

// TestCodePreservesProjectLifecycleErrors guards finding 8: this shared
// classifier is what both the CLI (errorCode/exitCode) and MCP (rpcFail)
// call, so unwrapping *projectlifecycle.Error here -- including when it's
// wrapped by fmt.Errorf("%w", ...) -- is what keeps a project-lifecycle
// error's real code reported identically over both transports instead of
// MCP falling back to a generic default the CLI doesn't.
func TestCodePreservesProjectLifecycleErrors(t *testing.T) {
	tests := []struct {
		code           projectlifecycle.ErrorCode
		wantExitStatus int
	}{
		{projectlifecycle.CodeUpgradeRequired, 11},
		{projectlifecycle.CodeProjectTooNew, 11},
		{projectlifecycle.CodeUpgradeUnsupported, 11},
		{projectlifecycle.CodeUpgradeFailed, 12},
	}
	for _, test := range tests {
		lifecycleErr := &projectlifecycle.Error{Code: test.code, Message: "boom"}
		if got := Code(lifecycleErr); got != string(test.code) {
			t.Errorf("direct: code %q, want %q", got, test.code)
		}
		if got := ExitStatus(lifecycleErr); got != test.wantExitStatus {
			t.Errorf("direct: exit status %d, want %d", got, test.wantExitStatus)
		}
		wrapped := fmt.Errorf("reconcile project: %w", lifecycleErr)
		if got := Code(wrapped); got != string(test.code) {
			t.Errorf("wrapped: code %q, want %q", got, test.code)
		}
	}
}

func TestCodeClassifiesBoundaryErrors(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{errors.New("credential not found"), "AUTHORIZATION"},
		{errors.New("signature verification failed"), "INTEGRITY"},
		{errors.New("remote transport failed"), "EXTERNAL"},
		{errors.New("invalid value"), "VALIDATION"},
	}
	for _, test := range tests {
		if got := Code(test.err); got != test.code {
			t.Errorf("%q classified as %q, want %q", test.err, got, test.code)
		}
	}
}
