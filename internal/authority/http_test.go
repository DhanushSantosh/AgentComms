package authority

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdmissionControlRejectsExcessRequest(t *testing.T) {
	server := &HTTPServer{admission: make(chan struct{}, 1), rates: newRateRegistry(1, 1)}
	server.admission <- struct{}{}
	handler := server.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("saturated request reached handler")
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", recorder.Code)
	}
}

func TestStreamUsesDedicatedAdmissionPool(t *testing.T) {
	server := &HTTPServer{admission: make(chan struct{}, 1), streamAdmission: make(chan struct{}, 1), rates: newRateRegistry(1, 1)}
	server.admission <- struct{}{}
	passed := false
	handler := server.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { passed = true }))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/projects/p/stream", nil))
	if !passed {
		t.Fatal("stream was blocked by normal request admission")
	}
	passed = false
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/not-a-route/stream", nil))
	if passed {
		t.Fatal("non-stream route bypassed normal request admission")
	}
	server.streamAdmission <- struct{}{}
	recorder := httptest.NewRecorder()
	server.stream(recorder, httptest.NewRequest(http.MethodGet, "/v1/projects/p/stream", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("full stream pool status=%d", recorder.Code)
	}
}

func TestBearerTokenProtectsAuthorityEndpoints(t *testing.T) {
	server := &HTTPServer{admission: make(chan struct{}, 1), streamAdmission: make(chan struct{}, 1), rates: newRateRegistry(1, 1), bearerToken: "secret-token"}
	passed := false
	handler := server.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { passed = true }))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/projects/p/state", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", recorder.Code)
	}
	if passed {
		t.Fatal("unauthorized request reached handler")
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/projects/p/state", nil)
	request.Header.Set("Authorization", "Bearer secret-token")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("authorized status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestBearerTokenLeavesHealthChecksPublic(t *testing.T) {
	server := &HTTPServer{admission: make(chan struct{}, 1), streamAdmission: make(chan struct{}, 1), rates: newRateRegistry(1, 1), bearerToken: "secret-token"}
	handler := server.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	for _, path := range []string{"/health/live", "/health/ready"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, recorder.Code)
		}
	}
}

func TestRateRegistry(t *testing.T) {
	registry := newRateRegistry(1, 1)
	now := time.Unix(1_000, 0)
	if allowed, _ := registry.allow("actor", now); !allowed {
		t.Fatal("first request rejected")
	}
	if allowed, wait := registry.allow("actor", now); allowed || wait <= 0 {
		t.Fatalf("second request allowed=%t wait=%s", allowed, wait)
	}
	if allowed, _ := registry.allow("actor", now.Add(time.Second)); !allowed {
		t.Fatal("refilled request rejected")
	}
}

// A peer in an environment you do not control -- a hosted agent reachable
// only through its own chat -- cannot be handed the shared authority token
// privately. Issuing it its own token makes a leak cost one identity and one
// restart to revoke, instead of rotating the credential every participant
// holds.
func TestAuthorizedAcceptsPerPeerTokensAndStillRejectsOthers(t *testing.T) {
	server := &HTTPServer{
		bearerToken: "primary-token",
		extraTokens: normalizeTokenSet(map[string]string{
			"cloud-agent": "cloud-token",
			"laptop":      "laptop-token",
		}),
	}
	for _, accepted := range []string{"primary-token", "cloud-token", "laptop-token"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/projects/p/events", nil)
		request.Header.Set("Authorization", "Bearer "+accepted)
		if !server.authorized(request) {
			t.Fatalf("token %q should authenticate", accepted)
		}
	}
	for _, rejected := range []string{"", "wrong-token", "cloud-token-with-suffix", "primary"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/projects/p/events", nil)
		if rejected != "" {
			request.Header.Set("Authorization", "Bearer "+rejected)
		}
		if server.authorized(request) {
			t.Fatalf("token %q must not authenticate", rejected)
		}
	}
}

// Revoking one peer must not disturb the others: drop its entry and its
// token stops working while every other credential keeps working.
func TestRevokingOnePeerTokenLeavesTheRestValid(t *testing.T) {
	server := &HTTPServer{extraTokens: normalizeTokenSet(map[string]string{
		"cloud-agent": "cloud-token", "laptop": "laptop-token",
	})}
	delete(server.extraTokens, "cloud-agent")

	revoked := httptest.NewRequest(http.MethodGet, "/v1/projects/p/events", nil)
	revoked.Header.Set("Authorization", "Bearer cloud-token")
	if server.authorized(revoked) {
		t.Fatal("a revoked peer token must stop authenticating")
	}
	kept := httptest.NewRequest(http.MethodGet, "/v1/projects/p/events", nil)
	kept.Header.Set("Authorization", "Bearer laptop-token")
	if !server.authorized(kept) {
		t.Fatal("revoking one peer must not invalidate the others")
	}
}

// A half-filled environment variable must not leave the authority open, and
// must never authorize the empty string.
func TestNormalizeTokenSetDropsBlanksRatherThanAuthorizingThem(t *testing.T) {
	got := normalizeTokenSet(map[string]string{"": "orphan-token", "label": "   ", "ok": " real-token "})
	if len(got) != 1 || got["ok"] != "real-token" {
		t.Fatalf("blank labels and blank tokens must be dropped, got %#v", got)
	}
	if normalizeTokenSet(map[string]string{"a": " ", "": ""}) != nil {
		t.Fatal("an all-blank set must normalize to nil, not an empty-but-present set")
	}
	// With no tokens configured at all the authority is open by design
	// (local development); that must not change here.
	open := &HTTPServer{}
	if !open.authorized(httptest.NewRequest(http.MethodGet, "/v1/projects/p/events", nil)) {
		t.Fatal("an authority with no tokens configured should stay open")
	}
}
