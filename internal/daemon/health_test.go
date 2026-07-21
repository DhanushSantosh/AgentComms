package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
)

func TestHealthPublishesDaemonProtocolVersion(t *testing.T) {
	instance := &Daemon{runtimeMode: "personal", projectID: "project"}
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	response := httptest.NewRecorder()
	instance.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	var health struct {
		ProtocolVersion int `json:"protocol_version"`
	}
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health.ProtocolVersion != controlplane.LocalDaemonProtocolVersion {
		t.Fatalf("protocol=%d, want %d", health.ProtocolVersion, controlplane.LocalDaemonProtocolVersion)
	}
}
