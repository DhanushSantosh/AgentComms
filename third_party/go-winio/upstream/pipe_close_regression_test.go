//go:build windows

package winio

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestClosePreservesCancellationAcrossConnectErrors(t *testing.T) {
	for _, completionErr := range []error{windows.ERROR_NO_DATA, windows.ERROR_PIPE_NOT_CONNECTED, windows.ERROR_OPERATION_ABORTED} {
		t.Run(fmt.Sprint(completionErr), func(t *testing.T) {
			// Keep real first-instance and overlapped handles. Only control the
			// completion error and ordering, not the cancellation state machine.
			path := fmt.Sprintf(`\\.\pipe\agc-close-regression-%d`, time.Now().UnixNano())
			h, err := makeServerPipeHandle(path, nil, &PipeConfig{}, true)
			if err != nil {
				t.Fatal(err)
			}
			defer windows.Close(h)
			listener := &win32PipeListener{path: path, closeCh: make(chan int), doneCh: make(chan int)}
			started := make(chan *win32File, 1)
			release := make(chan struct{})
			result := make(chan error, 1)
			go func() {
				_, err := listener.makeConnectedServerPipeWithConnector(func(p *win32File) error {
					started <- p
					<-release
					return completionErr
				})
				result <- err
			}()
			var pending *win32File
			select {
			case pending = <-started:
			case err := <-result:
				t.Fatalf("creating pending pipe: %v", err)
			case <-time.After(time.Second):
				t.Fatal("connector never started")
			}
			closed := make(chan struct{})
			go func() { _ = listener.Close(); close(closed) }()
			deadline := time.Now().Add(time.Second)
			for !pending.IsClosed() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			cancelled := pending.IsClosed()
			close(release)
			err = <-result
			// Match listenerRoutine's exit decision, then unblock Close even on
			// failure so this regression leaves no blocked goroutine behind.
			preserved := errors.Is(err, ErrPipeListenerClosed)
			close(listener.doneCh)
			<-closed
			if !cancelled {
				t.Fatal("Close did not cancel the pending handle")
			}
			if !preserved {
				t.Fatalf("close request lost after connect completion %v: got %v, want ErrPipeListenerClosed", completionErr, err)
			}
		})
	}
}
