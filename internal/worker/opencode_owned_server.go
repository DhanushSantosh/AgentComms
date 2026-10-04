package worker

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/opencodeclient"
)

// ownedLiveServer retains process ownership, not just a cached URL. A worker
// keeps it across invocations; closing it kills/reaps only its owned tree.
type ownedLiveServer struct {
	baseURL string
	cancel  context.CancelFunc
	done    chan struct{}
	err     error // published by closing done
	once    sync.Once
}

func startOpenCodeOwnedServer(ctx context.Context, workDir string) (*ownedLiveServer, error) {
	cmd := exec.Command("opencode", "serve", "--hostname", "127.0.0.1", "--port", "0")
	cmd.Dir = workDir
	return startOwnedLiveServer(ctx, cmd, workDir)
}

func startOwnedLiveServer(ctx context.Context, cmd *exec.Cmd, workDir string) (*ownedLiveServer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	server := &ownedLiveServer{cancel: cancel, done: make(chan struct{})}
	output := &liveServerOutput{ready: make(chan string, 1)}
	cmd.Stdout, cmd.Stderr = output, output
	go func() { server.err = runOwnedCommand(lifetime, cmd); close(server.done) }()
	startup, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	fail := func(err error) (*ownedLiveServer, error) { return nil, errors.Join(err, server.Close()) }
	select {
	case server.baseURL = <-output.ready:
		address, err := url.Parse(server.baseURL)
		if err != nil || address.Scheme != "http" || address.Hostname() != "127.0.0.1" || address.User != nil || address.RawQuery != "" || address.Fragment != "" || address.Path != "" {
			return fail(errors.New("owned OpenCode server reported an invalid loopback endpoint"))
		}
		port, err := strconv.Atoi(address.Port())
		if err != nil || port < 1 || port > 65535 {
			return fail(errors.New("owned OpenCode server reported an invalid port"))
		}
	case <-server.done:
		return fail(errors.New("owned OpenCode server exited during startup"))
	case <-startup.Done():
		return fail(startup.Err())
	}
	client := opencodeclient.New(server.baseURL, workDir)
	for {
		select {
		case <-server.done:
			return fail(errors.New("owned OpenCode server exited before readiness"))
		default:
		}
		probe, stop := context.WithTimeout(startup, 500*time.Millisecond)
		err := client.Health(probe)
		stop()
		if err == nil {
			return server, nil
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-server.done:
			return fail(errors.New("owned OpenCode server exited before readiness"))
		case <-startup.Done():
			return fail(startup.Err())
		}
	}
}

func (s *ownedLiveServer) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(s.cancel)
	<-s.done
	return withoutOwnedCancellation(s.err)
}

// Keep actual tree-cleanup failures even when cancellation is expected.
func withoutOwnedCancellation(err error) error {
	if err == nil || err == context.Canceled {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var retained []error
		for _, child := range joined.Unwrap() {
			if actual := withoutOwnedCancellation(child); actual != nil {
				retained = append(retained, actual)
			}
		}
		return errors.Join(retained...)
	}
	return err
}

// Continuously drain output, retaining only an incomplete bounded line for
// startup parsing. Provider logs/credentials are never put into status errors.
type liveServerOutput struct {
	mu      sync.Mutex
	pending string
	ready   chan string
	found   bool
}

func (o *liveServerOutput) Write(data []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.found {
		return len(data), nil
	}
	o.pending += string(data)
	for {
		line, rest, ok := strings.Cut(o.pending, "\n")
		if !ok {
			break
		}
		o.pending = rest
		if address, ok := opencodeclient.ParseListeningURL(line); ok {
			o.found = true
			o.pending = ""
			o.ready <- address
			return len(data), nil
		}
	}
	if len(o.pending) > 65536 {
		o.pending = o.pending[len(o.pending)-65536:]
	}
	return len(data), nil
}
