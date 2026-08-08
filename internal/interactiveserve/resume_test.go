package interactiveserve

import (
	"reflect"
	"testing"
)

func TestPinResumeArgsReplacesImplicitContinueWithExplicitResume(t *testing.T) {
	got := PinResumeArgs([]string{"claude", "--dangerously-skip-permissions", "--continue"}, "sess-1")
	want := []string{"claude", "--dangerously-skip-permissions", "--resume", "sess-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPinResumeArgsStripsShortContinueFlag(t *testing.T) {
	got := PinResumeArgs([]string{"claude", "-c"}, "sess-1")
	want := []string{"claude", "--resume", "sess-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPinResumeArgsAppendsExplicitFlagWhenNoImplicitOnePresent(t *testing.T) {
	// A bare `opencode` invocation (no --continue/-c typed at all) has
	// nothing for the implicit-flag stripping loop to find, but should
	// still gain the explicit --session <id> unconditionally.
	got := PinResumeArgs([]string{"opencode"}, "sess-1")
	want := []string{"opencode", "--session", "sess-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPinResumeArgsForOpencode(t *testing.T) {
	got := PinResumeArgs([]string{"opencode", "--continue"}, "sess-1")
	want := []string{"opencode", "--session", "sess-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPinResumeArgsRespectsExistingExplicitFlag(t *testing.T) {
	// An operator who already typed --resume/--session/--conversation by
	// hand is never second-guessed, even if the value differs from the
	// pinned binding.
	in := []string{"claude", "--resume", "operator-chosen-session"}
	got := PinResumeArgs(in, "pinned-session")
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("expected args untouched, got %v", got)
	}
}

func TestPinResumeArgsNoOpWithoutSessionID(t *testing.T) {
	in := []string{"claude", "--continue"}
	got := PinResumeArgs(in, "")
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("expected args untouched without a sessionID, got %v", got)
	}
}

func TestPinResumeArgsNoOpForUnknownAdapter(t *testing.T) {
	in := []string{"bash", "-c", "true"}
	got := PinResumeArgs(in, "sess-1")
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("expected args untouched for an unknown adapter, got %v", got)
	}
}

func TestPinResumeArgsNoOpForEmptyCommand(t *testing.T) {
	got := PinResumeArgs(nil, "sess-1")
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestPinResumeArgsResolvesAdapterByBasenameNotFullPath(t *testing.T) {
	got := PinResumeArgs([]string{"/usr/local/bin/claude", "--continue"}, "sess-1")
	want := []string{"/usr/local/bin/claude", "--resume", "sess-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
