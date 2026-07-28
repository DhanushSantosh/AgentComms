package authority

import (
	"encoding/json"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// TestDecodePayloadCoversEveryRegisteredEventType is a regression test for a
// real bug: this file's own decodePayload switch (a second, manual registry
// separate from model.payloadFactories, needed because personalauthority's
// equivalent is generic via reflection but this one narrows via a type
// switch) silently lacked a case for *model.AgentRenamed since the commit
// that introduced agent.rename -- every rename through this backend failed
// with "unsupported payload," undetected because it never ran in CI
// (integration tests here require a live AGENT_COMMS_TEST_POSTGRES_URL).
// This test needs no database: it just proves every event type
// model.RegisteredEventTypes lists has a matching case here, so the next
// missing type fails at `go test` time instead of shipping silently.
func TestDecodePayloadCoversEveryRegisteredEventType(t *testing.T) {
	for _, typ := range model.RegisteredEventTypes() {
		t.Run(typ, func(t *testing.T) {
			if _, err := decodePayload(typ, json.RawMessage("{}")); err != nil {
				t.Fatalf("decodePayload has no case for registered event type %q: %v", typ, err)
			}
		})
	}
}
