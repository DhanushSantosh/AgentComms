package remote

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
)

func TestClientMapsStableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"CONFLICT","message":"lease conflict","retry_after_ms":25}}`))
	}))
	defer server.Close()
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.Command(context.Background(), controlplane.Command{ProjectID: "project"})
	controlErr, ok := err.(*controlplane.Error)
	if !ok || controlErr.Code != controlplane.CodeConflict || controlErr.RetryAfter != 25*time.Millisecond {
		t.Fatalf("error=%#v", err)
	}
}

func TestClientAddsBearerToken(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("authorization header=%q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
			Header:     make(http.Header),
		}, nil
	})
	client, err := NewWithToken("https://authority.example", time.Second, " secret-token ")
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = transport
	if err = client.CreateProject(context.Background(), "project", "owner"); err != nil {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
