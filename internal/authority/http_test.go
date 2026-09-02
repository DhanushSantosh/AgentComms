package authority

import (
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
