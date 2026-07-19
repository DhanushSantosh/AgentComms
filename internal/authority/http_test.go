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
