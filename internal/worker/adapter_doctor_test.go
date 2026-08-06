package worker

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeCLI writes a throwaway executable script at dir/name that just prints
// helpOutput to stdout and exits 0, standing in for a real provider binary
// so these tests don't depend on any specific CLI actually being installed.
func fakeCLI(t *testing.T, dir, name, helpOutput string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fakeCLI writes a POSIX shell script")
	}
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\ncat <<'EOF'\n" + helpOutput + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeAdapterSource(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAssumedFlagsExtractsFlagLiteralsFromSource(t *testing.T) {
	dir := t.TempDir()
	writeAdapterSource(t, dir, "adapter_fixture.go", `package worker
func fixtureArguments() []string {
	return []string{"--print", "--output-format", "text"}
}
`)
	orig := adapterSourceFile["fixture"]
	adapterSourceFile["fixture"] = "adapter_fixture.go"
	defer func() {
		if orig == "" {
			delete(adapterSourceFile, "fixture")
		} else {
			adapterSourceFile["fixture"] = orig
		}
	}()

	flags, err := AssumedFlags("fixture", dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--output-format", "--print"}
	if len(flags) != len(want) {
		t.Fatalf("got %v, want %v", flags, want)
	}
	for i, f := range want {
		if flags[i] != f {
			t.Fatalf("got %v, want %v", flags, want)
		}
	}
}

func TestAssumedFlagsRejectsUnknownAdapter(t *testing.T) {
	if _, err := AssumedFlags("no-such-adapter", t.TempDir()); err == nil {
		t.Fatal("expected an error for an adapter with no known source file")
	}
}

func TestDocumentedFlagsExtractsFlagsFromHelpOutput(t *testing.T) {
	dir := t.TempDir()
	executable := fakeCLI(t, dir, "fake-cli", "Usage:\n  --print         run once\n  --model string  model name\n")
	flags, err := DocumentedFlags(context.Background(), executable)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", "--print"}
	if len(flags) != len(want) {
		t.Fatalf("got %v, want %v", flags, want)
	}
	for i, f := range want {
		if flags[i] != f {
			t.Fatalf("got %v, want %v", flags, want)
		}
	}
}

// TestVerifyAdapterFlagsReportsGenuinelyMissingFlags is the regression test
// for the class of bug this tool exists to catch: an adapter's own source
// assumes a flag the real, installed binary's --help never documents.
// Confirmed live against the real agy binary before this test was written:
// injecting a fake "--totally-fake-flag-xyz" into adapter_agy.go's actual
// Arguments() code (not a comment) made the real CLI command report it as
// missing; reverting made it disappear again.
func TestVerifyAdapterFlagsReportsGenuinelyMissingFlags(t *testing.T) {
	dir := t.TempDir()
	writeAdapterSource(t, dir, "adapter_fixture.go", `package worker
func fixtureArguments() []string {
	return []string{"--print", "--nonexistent-flag"}
}
`)
	orig := adapterSourceFile["fixture"]
	adapterSourceFile["fixture"] = "adapter_fixture.go"
	defer func() {
		if orig == "" {
			delete(adapterSourceFile, "fixture")
		} else {
			adapterSourceFile["fixture"] = orig
		}
	}()

	executable := fakeCLI(t, dir, "fake-cli", "Usage:\n  --print   run once\n")
	missing, err := VerifyAdapterFlags(context.Background(), "fixture", dir, executable)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != "--nonexistent-flag" {
		t.Fatalf("expected exactly [--nonexistent-flag] missing, got %v", missing)
	}
}

func TestVerifyAdapterFlagsIsCleanWhenEverythingIsDocumented(t *testing.T) {
	dir := t.TempDir()
	writeAdapterSource(t, dir, "adapter_fixture.go", `package worker
func fixtureArguments() []string {
	return []string{"--print", "--model"}
}
`)
	orig := adapterSourceFile["fixture"]
	adapterSourceFile["fixture"] = "adapter_fixture.go"
	defer func() {
		if orig == "" {
			delete(adapterSourceFile, "fixture")
		} else {
			adapterSourceFile["fixture"] = orig
		}
	}()

	executable := fakeCLI(t, dir, "fake-cli", "Usage:\n  --print   run once\n  --model   model name\n")
	missing, err := VerifyAdapterFlags(context.Background(), "fixture", dir, executable)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("expected no missing flags, got %v", missing)
	}
}
