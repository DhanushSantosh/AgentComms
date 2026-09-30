//go:build windows

package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
)

func TestListenLocalShutdownReleasesEndpointDuringClientDisconnects(t *testing.T) {
	endpoint := fmt.Sprintf(`\\.\pipe\agc-shutdown-%d-%d`, os.Getpid(), time.Now().UnixNano())
	for round := 0; round < 40; round++ {
		listener, err := ListenLocal(endpoint)
		if err != nil {
			t.Fatalf("round %d rebind: %v", round, err)
		}
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })}
		served := make(chan error, 1)
		go func() { served <- server.Serve(listener) }()
		ctx, cancel := context.WithCancel(context.Background())
		var clients sync.WaitGroup
		connected := make(chan struct{}, 1)
		for client := 0; client < 8; client++ {
			clients.Add(1)
			go func() {
				defer clients.Done()
				for ctx.Err() == nil {
					conn, err := winio.DialPipeContext(ctx, endpoint)
					if err != nil {
						return
					}
					select {
					case connected <- struct{}{}:
					default:
					}
					// Disconnect during accept/close, without an HTTP request.
					_ = conn.Close()
				}
			}()
		}
		select {
		case <-connected:
		case <-time.After(daemonShutdownTimeout):
			cancel()
			_ = listener.Close()
			clients.Wait()
			t.Fatal("no fixture client connected")
		}
		stopped := make(chan error, 1)
		go func() {
			shutdownCtx, stop := context.WithTimeout(context.Background(), daemonShutdownTimeout)
			defer stop()
			stopped <- server.Shutdown(shutdownCtx)
		}()
		cancel()
		clients.Wait()
		select {
		case err := <-stopped:
			if err != nil {
				t.Fatalf("round %d shutdown: %v", round, err)
			}
		case <-time.After(2 * daemonShutdownTimeout):
			t.Fatalf("round %d listener Close ignored shutdown deadline", round)
		}
		if err := <-served; !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("round %d Serve: %v", round, err)
		}
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
	}
	// The final listener, not only intermediate ones, must release its name.
	listener, err := ListenLocal(endpoint)
	if err != nil {
		t.Fatalf("final rebind: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}
