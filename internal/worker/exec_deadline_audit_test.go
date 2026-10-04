package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// Regression for inherited output retaining an invocation after its deadline.
// Child exits independently after two seconds; no persistent provider is used.
func TestExecDeadlineWithInheritedOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell reproducer")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep unavailable")
	}
	for _, adapter := range []string{"codex", "opencode"} {
		for _, inherited := range []bool{true, false} {
			name := "closed-output"
			if inherited {
				name = "inherited-output"
			}
			t.Run(adapter+"/"+name, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "provider")
				child := "sleep 2 &"
				if !inherited {
					child = "sleep 2 >/dev/null 2>&1 &"
				}
				if err := os.WriteFile(path, []byte("#!/bin/sh\n"+child+"\nwait\n"), 0o700); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				started := time.Now()
				var err error
				if adapter == "codex" {
					_, err = runCLIAdapter(ctx, Config{Executable: path, WorkDir: root}, codexAdapter{}, model.Invocation{})
				} else {
					_, _, err = runOpenCode(ctx, Config{Executable: path, WorkDir: root}, model.Invocation{}, "")
				}
				if err == nil || ctx.Err() == nil {
					t.Fatal("execution did not reach deadline")
				}
				if elapsed := time.Since(started); elapsed > time.Second {
					t.Fatalf("100ms execution deadline returned after %s with %v", elapsed, err)
				}
			})
		}
	}
}
