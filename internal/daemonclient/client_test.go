package daemonclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// roundTripFunc adapts a function to http.RoundTripper, letting the test
// supply a canned response with no real networking involved -- mirroring
// the kind of in-process transport NewWithTransport is meant to accept.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestNewWithTransportRoundTrip(t *testing.T) {
	const body = `{"status":"ok","runtime_mode":"test","project_id":"proj-1","protocol_version":1,"product_version":"v0","build_id":"b1","project_format_version":1,"cache_schema_version":1,"draft_schema_version":1}`

	var gotPath string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		gotPath = request.URL.Path
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})

	client, err := NewWithTransport("in-process", time.Second, transport)
	if err != nil {
		t.Fatalf("NewWithTransport: %v", err)
	}

	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}

	if gotPath != "/health/live" {
		t.Fatalf("unexpected request path: %q", gotPath)
	}
	if health.Status != "ok" || health.ProjectID != "proj-1" {
		t.Fatalf("unexpected health payload: %+v", health)
	}
}

func TestNewWithTransportRequiresEndpoint(t *testing.T) {
	if _, err := NewWithTransport("", time.Second, http.DefaultTransport); err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}
