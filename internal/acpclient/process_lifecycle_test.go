package acpclient

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

func TestMain(m *testing.M) {
	if os.Getenv("AGC_ACP_PROCESS_HELPER") == "1" {
		agent := &fakeAgent{}
		conn := acpsdk.NewAgentSideConnection(agent, os.Stdout, os.Stdin)
		agent.conn = conn
		select {
		case <-conn.Done():
		case <-time.After(20 * time.Second):
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSpawnedACPCloseReapsProcessAndClosesConnection(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config := baseConfig(t)
	config.Command, config.Dir = executable, config.Cwd
	config.Env = append(os.Environ(), "AGC_ACP_PROCESS_HELPER=1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := Dial(ctx, config, "")
	if err != nil {
		t.Fatal(err)
	}
	// Cleanup reaps the pre-fix child too, so the red test does not leak it.
	t.Cleanup(func() {
		_ = session.cmd.Process.Kill()
		if session.cmd.ProcessState == nil {
			_ = session.cmd.Wait()
		}
	})
	if text, _, err := session.Prompt(ctx, "synthetic"); err != nil || text != "Hello, world." {
		t.Fatalf("prompt: %q %v", text, err)
	}
	var closes sync.WaitGroup
	errors := make(chan error, 8)
	for range 8 {
		closes.Add(1)
		go func() { defer closes.Done(); errors <- session.Close() }()
	}
	closes.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if session.cmd.ProcessState == nil {
		t.Fatal("Close did not reap the spawned ACP process")
	}
	select {
	case <-session.conn.Done():
	case <-time.After(time.Second):
		t.Fatal("Close left ACP connection open")
	}
	if err := session.Close(); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
}
