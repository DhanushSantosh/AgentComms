//go:build darwin

package terminallaunch

import "testing"

func TestBuildScriptEscapesQuotesAndBackslashes(t *testing.T) {
	script := buildScript("/tmp/a b", []string{"agent-comms", `arg"with"quotes`, `back\slash`})
	want := `tell application "Terminal" to do script "cd '/tmp/a b' && exec 'agent-comms' 'arg\"with\"quotes' 'back\\slash'"`
	if script != want {
		t.Fatalf("got:\n%s\nwant:\n%s", script, want)
	}
}

func TestOpenRejectsEmptyArgv(t *testing.T) {
	if err := Open("/proj", nil); err == nil {
		t.Fatal("expected an error for an empty command")
	}
}
