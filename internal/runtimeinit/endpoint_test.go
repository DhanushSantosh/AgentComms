package runtimeinit

import (
	"strings"
	"testing"
)

func TestDaemonEndpointIsDeterministicAndProjectScoped(t *testing.T) {
	first := DaemonEndpoint("/projects/first", "project-one")
	second := DaemonEndpoint("/projects/second", "project-one")
	if first != second {
		t.Fatalf("project location changed daemon endpoint: %q vs %q", first, second)
	}
	if first == DaemonEndpoint("/projects/first", "project-two") {
		t.Fatal("different projects received the same daemon endpoint")
	}
}

func TestDaemonEndpointDoesNotDependOnProcessTempDirectory(t *testing.T) {
	firstTemporaryDirectory := t.TempDir()
	secondTemporaryDirectory := t.TempDir()
	t.Setenv("TMPDIR", firstTemporaryDirectory)
	first := DaemonEndpoint("/projects/shared", "project")
	t.Setenv("TMPDIR", secondTemporaryDirectory)
	second := DaemonEndpoint("/projects/shared", "project")
	if first != second {
		t.Fatalf("TMPDIR changed daemon endpoint: %q vs %q", first, second)
	}
	if strings.Contains(first, firstTemporaryDirectory) || strings.Contains(first, secondTemporaryDirectory) {
		t.Fatalf("daemon endpoint unexpectedly used a test temporary directory: %q", first)
	}
}
