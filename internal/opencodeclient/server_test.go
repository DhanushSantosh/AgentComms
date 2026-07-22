package opencodeclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseListeningURL(t *testing.T) {
	cases := []struct {
		line string
		want string
		ok   bool
	}{
		{"opencode server listening on http://127.0.0.1:4098", "http://127.0.0.1:4098", true},
		{"timestamp=2026-07-22T05:36:25.898Z level=INFO message=loading path=/x", "", false},
		{"opencode server listening on https://0.0.0.0:9000", "https://0.0.0.0:9000", true},
	}
	for _, tc := range cases {
		got, ok := parseListeningURL(tc.line)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseListeningURL(%q) = (%q, %v), want (%q, %v)", tc.line, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLoadServerInfoMissingFileReturnsError(t *testing.T) {
	if _, err := loadServerInfo("/nonexistent/path/opencode-server.json"); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestSaveAndLoadServerInfoRoundTrips(t *testing.T) {
	path := ServerInfoPath(t.TempDir())
	if err := saveServerInfo(path, ServerInfo{BaseURL: "http://127.0.0.1:4096"}); err != nil {
		t.Fatal(err)
	}
	info, err := loadServerInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.BaseURL != "http://127.0.0.1:4096" {
		t.Fatalf("unexpected round-tripped info: %+v", info)
	}
}

func TestLoadServerInfoRejectsEmptyBaseURL(t *testing.T) {
	path := ServerInfoPath(t.TempDir())
	if err := saveServerInfo(path, ServerInfo{BaseURL: ""}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadServerInfo(path); err == nil {
		t.Fatal("expected an error for an empty base URL")
	}
}

func fakeOpenCodeServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestResolveRunningServerPrefersCache(t *testing.T) {
	cached := fakeOpenCodeServer(t)
	fallback := fakeOpenCodeServer(t)
	path := ServerInfoPath(t.TempDir())
	if err := saveServerInfo(path, ServerInfo{BaseURL: cached.URL}); err != nil {
		t.Fatal(err)
	}
	baseURL, ok := resolveRunningServer(context.Background(), path, fallback.URL)
	if !ok || baseURL != cached.URL {
		t.Fatalf("expected the cached server %q, got (%q, %v)", cached.URL, baseURL, ok)
	}
}

// TestResolveRunningServerFallsBackToDefaultPort covers exactly the gap
// that let a still-running opencode serve instance go untracked and orphaned
// after its cache file was lost: with no cache at all, a healthy server at
// the well-known default-port address must still be found and reused
// instead of triggering a duplicate spawn.
func TestResolveRunningServerFallsBackToDefaultPort(t *testing.T) {
	fallback := fakeOpenCodeServer(t)
	path := ServerInfoPath(t.TempDir())
	baseURL, ok := resolveRunningServer(context.Background(), path, fallback.URL)
	if !ok || baseURL != fallback.URL {
		t.Fatalf("expected the fallback server %q, got (%q, %v)", fallback.URL, baseURL, ok)
	}
}

func TestResolveRunningServerReportsNoneWhenNeitherResponds(t *testing.T) {
	path := ServerInfoPath(t.TempDir())
	if _, ok := resolveRunningServer(context.Background(), path, "http://127.0.0.1:1"); ok {
		t.Fatal("expected no running server to be found")
	}
}
