package failure

import (
	"errors"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
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
