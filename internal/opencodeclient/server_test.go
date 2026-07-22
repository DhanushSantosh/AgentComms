package opencodeclient

import (
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
