package worker

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"
)

// Output draining is a cleanup bound, not an extension of provider execution.
const execOutputDrainTimeout = time.Second

// runOwnedCommand supervises only the invocation's process group/job. Shared
// live brokers intentionally do not use this invocation-owned lifecycle.
func runOwnedCommand(ctx context.Context, command *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	owned, err := prepareOwnedExec(command)
	if err != nil {
		return err
	}
	defer owned.release()
	command.WaitDelay = execOutputDrainTimeout
	if err := command.Start(); err != nil {
		return err
	}
	if err := owned.started(ctx, command); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	var once sync.Once
	var cleanupErr error
	terminate := func() {
		once.Do(func() {
			cleanupErr = owned.terminate()
			if cleanupErr != nil {
				// Still reap the direct process and bound pipe draining when
				// tree cleanup fails; preserve that failure for the caller.
				cleanupErr = errors.Join(cleanupErr, command.Process.Kill())
			}
		})
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			terminate()
		case <-stop:
		}
	}()
	runErr := command.Wait()
	close(stop)
	<-done
	terminate()
	if ctx.Err() != nil {
		return errors.Join(ctx.Err(), cleanupErr)
	}
	return errors.Join(runErr, cleanupErr)
}
