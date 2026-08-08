package app

import "testing"

func TestCommandDocumentationIncludesVisibleSurfaceAndExcludesDaemon(t *testing.T) {
	documents := CommandDocumentation()
	foundInvocation := false
	for _, document := range documents {
		switch document.Path {
		case "agent-comms invocation request":
			foundInvocation = true
		case "agent-comms daemon", "agent-comms daemon serve":
			t.Fatalf("hidden daemon command leaked into documentation: %s", document.Path)
		}
	}
	if !foundInvocation {
		t.Fatal("invocation request command missing from documentation")
	}
}
