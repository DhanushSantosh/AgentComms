package contamination

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestCoverageFloorRequiresEveryMeasurement(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the Bash coverage gate runs on Linux CI")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "coverage-floor.sh"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	entries := regexp.MustCompile(`(?m)^\s*\[([^]]+)\]=[0-9]+$`).FindAllStringSubmatch(string(raw), -1)
	if len(entries) == 0 {
		t.Fatal("coverage gate has no declared floors")
	}
	var lines []string
	for _, entry := range entries {
		lines = append(lines, fmt.Sprintf("ok  \t%s\t0.001s\tcoverage: 100.0%% of statements", entry[1]))
	}
	all := strings.Join(lines, "\n") + "\n"
	for _, tc := range []struct {
		name, output string
		goStatus     int
		wantStatus   int
	}{
		{"all measurements", all, 0, 0},
		{"missing package", strings.Join(lines[1:], "\n") + "\n", 0, 1},
		{"empty output", "", 0, 1},
		{"below floor", strings.Replace(all, "100.0%", "0.0%", 1), 0, 1},
		{"no tests", fmt.Sprintf("\t%s\tcoverage: 0.0%% of statements\n", entries[0][1]) + strings.Join(lines[1:], "\n"), 0, 1},
		{"upstream test failure", all, 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := t.TempDir()
			output := filepath.Join(fixture, "coverage.txt")
			if err := os.WriteFile(output, []byte(tc.output), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(fixture, "go"), []byte("#!/bin/sh\ncat \"$AGC_COVERAGE_TEST_OUTPUT\"\nexit \"$AGC_COVERAGE_TEST_STATUS\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(bash, script)
			cmd.Env = append(os.Environ(),
				"PATH="+fixture+string(os.PathListSeparator)+os.Getenv("PATH"),
				"AGC_COVERAGE_TEST_OUTPUT="+output,
				fmt.Sprintf("AGC_COVERAGE_TEST_STATUS=%d", tc.goStatus))
			result, err := cmd.CombinedOutput()
			status := 0
			if err != nil {
				exit, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				status = exit.ExitCode()
			}
			if status != tc.wantStatus {
				t.Fatalf("coverage gate exit=%d, want=%d:\n%s", status, tc.wantStatus, result)
			}
		})
	}
}
